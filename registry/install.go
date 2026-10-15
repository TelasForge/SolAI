// Package registry handles tool installation from GitHub releases.
package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// httpClient is used for all GitHub API and asset download requests.
var httpClient = &http.Client{Timeout: 5 * time.Minute}

// githubAPIBase is the GitHub API root, overridable in tests.
var githubAPIBase = "https://api.github.com"

// indexURL is the hosted curated tool registry, overridable in tests.
var indexURL = "https://raw.githubusercontent.com/melodyogonna/solai/main/registry/index.json"

// indexEntry is one tool entry from the hosted registry index.
type indexEntry struct {
	Latest   string                  `json:"latest"`
	Versions map[string]indexVersion `json:"versions"`
}

// indexVersion holds the per-version asset URLs from the registry index.
type indexVersion struct {
	Manifest  string            `json:"manifest"`
	Checksums string            `json:"checksums"`
	Assets    map[string]string `json:"assets"` // "linux/amd64" → download URL
}

// indexFile is the top-level structure of registry/index.json.
type indexFile struct {
	Tools map[string]indexEntry `json:"tools"`
}

// githubAsset is one entry from a GitHub release's assets list.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// githubRelease is the subset of a GitHub release API response we care about.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// manifestHeader is the minimal set of manifest.json fields needed at install time.
type manifestHeader struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Executable  string `json:"executable"`
}

// Install downloads a tool and places it in toolsDir.
//
// ref is one of:
//   - "name" or "name@tag"         — short name resolved via the curated registry index
//   - "owner/repo" or "owner/repo@tag" — installed directly from the GitHub release
//
// The tool binary is written to:
//
//	toolsDir/<name>/bin/<name>   (chmod 0755)
//	toolsDir/<name>/manifest.json
func Install(ref, toolsDir string) error {
	if strings.Contains(ref, "/") {
		return installFromGitHub(ref, toolsDir)
	}
	return installFromIndex(ref, toolsDir)
}

// IsInstalled reports whether a tool with the given name is already installed in toolsDir.
func IsInstalled(name, toolsDir string) bool {
	_, err := os.Stat(filepath.Join(toolsDir, name, "manifest.json"))
	return err == nil
}

// installFromIndex resolves a short tool name via the hosted registry index and installs it.
func installFromIndex(ref, toolsDir string) error {
	name, tag := parseShortRef(ref)
	if name == "" {
		return fmt.Errorf("tool name is empty")
	}

	if IsInstalled(name, toolsDir) {
		return fmt.Errorf("tool %q is already installed (run: solai uninstall %s to remove it first)", name, name)
	}

	indexData, err := downloadAsset(indexURL)
	if err != nil {
		return fmt.Errorf("fetching registry index: %w", err)
	}
	var idx indexFile
	if err := json.Unmarshal(indexData, &idx); err != nil {
		return fmt.Errorf("parsing registry index: %w", err)
	}

	entry, ok := idx.Tools[name]
	if !ok {
		return fmt.Errorf("tool %q not found in registry (use owner/repo for third-party tools)", name)
	}

	if tag == "" {
		tag = entry.Latest
	}
	ver, ok := entry.Versions[tag]
	if !ok {
		return fmt.Errorf("tool %q has no version %q in registry", name, tag)
	}

	platform := "linux/" + runtime.GOARCH
	binaryURL, ok := ver.Assets[platform]
	if !ok {
		return fmt.Errorf("tool %q@%s has no binary for %s", name, tag, platform)
	}
	binaryName := fmt.Sprintf("%s-linux-%s", name, runtime.GOARCH)

	manifestData, err := downloadAsset(ver.Manifest)
	if err != nil {
		return fmt.Errorf("downloading manifest.json: %w", err)
	}
	var m manifestHeader
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("parsing manifest.json: %w", err)
	}
	if m.Name != name {
		return fmt.Errorf("manifest name %q does not match requested tool %q", m.Name, name)
	}
	if err := validateManifest(m); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}

	binaryData, err := downloadAsset(binaryURL)
	if err != nil {
		return fmt.Errorf("downloading binary: %w", err)
	}

	if ver.Checksums != "" {
		checksumData, err := downloadAsset(ver.Checksums)
		if err != nil {
			return fmt.Errorf("downloading checksums.txt: %w", err)
		}
		if err := verifyChecksum(checksumData, binaryName, binaryData); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	return writeToolFiles(toolsDir, m.Name, manifestData, binaryData)
}

// installFromGitHub installs a tool directly from a GitHub release.
func installFromGitHub(ref, toolsDir string) error {
	owner, repo, tag := parseRef(ref)
	if owner == "" || repo == "" {
		return fmt.Errorf("invalid tool ref %q: expected owner/repo or owner/repo@tag", ref)
	}

	release, err := fetchRelease(owner, repo, tag)
	if err != nil {
		return fmt.Errorf("fetching release: %w", err)
	}

	manifestAsset := findAsset(release.Assets, "manifest.json")
	if manifestAsset == nil {
		return fmt.Errorf("release %s/%s@%s has no manifest.json asset", owner, repo, release.TagName)
	}
	manifestData, err := downloadAsset(manifestAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading manifest.json: %w", err)
	}

	var m manifestHeader
	if err := json.Unmarshal(manifestData, &m); err != nil {
		return fmt.Errorf("parsing manifest.json: %w", err)
	}
	if err := validateManifest(m); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	if IsInstalled(m.Name, toolsDir) {
		return fmt.Errorf("tool %q is already installed (run: solai uninstall %s to remove it first)", m.Name, m.Name)
	}

	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("%s-linux-%s", m.Name, arch)
	binaryAsset := findAsset(release.Assets, binaryName)
	if binaryAsset == nil {
		return fmt.Errorf("release has no binary asset %q for linux/%s", binaryName, arch)
	}
	binaryData, err := downloadAsset(binaryAsset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", binaryName, err)
	}

	if checksumAsset := findAsset(release.Assets, "checksums.txt"); checksumAsset != nil {
		checksumData, err := downloadAsset(checksumAsset.BrowserDownloadURL)
		if err != nil {
			return fmt.Errorf("downloading checksums.txt: %w", err)
		}
		if err := verifyChecksum(checksumData, binaryName, binaryData); err != nil {
			return fmt.Errorf("checksum verification failed: %w", err)
		}
	}

	return writeToolFiles(toolsDir, m.Name, manifestData, binaryData)
}

// validToolNameRE restricts tool names to lowercase letters, digits, and hyphens,
// starting with a letter or digit. This prevents path traversal attacks when
// the name is sourced from a remote manifest.
var validToolNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func validateToolName(name string) error {
	if !validToolNameRE.MatchString(name) {
		return fmt.Errorf("unsafe tool name %q: must match [a-z0-9][a-z0-9-]*", name)
	}
	return nil
}

// writeToolFiles persists manifest.json and the binary into toolsDir/<name>/.
func writeToolFiles(toolsDir, name string, manifestData, binaryData []byte) error {
	if err := validateToolName(name); err != nil {
		return err
	}
	toolDir := filepath.Join(toolsDir, name)
	binDir := filepath.Join(toolDir, "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return fmt.Errorf("creating tool directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "manifest.json"), manifestData, 0644); err != nil {
		return fmt.Errorf("writing manifest.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, name), binaryData, 0755); err != nil {
		return fmt.Errorf("writing binary: %w", err)
	}
	return nil
}

// parseShortRef splits a short ref "name[@tag]" into name and optional tag.
func parseShortRef(ref string) (name, tag string) {
	parts := strings.SplitN(ref, "@", 2)
	name = parts[0]
	if len(parts) == 2 {
		tag = parts[1]
	}
	return
}

// parseRef splits "owner/repo[@tag]" into its components.
func parseRef(ref string) (owner, repo, tag string) {
	parts := strings.SplitN(ref, "@", 2)
	repoRef := parts[0]
	if len(parts) == 2 {
		tag = parts[1]
	}
	slash := strings.SplitN(repoRef, "/", 2)
	if len(slash) == 2 {
		owner = slash[0]
		repo = slash[1]
	}
	return
}

// fetchRelease retrieves release metadata from the GitHub API.
// If tag is empty the latest release is fetched.
func fetchRelease(owner, repo, tag string) (githubRelease, error) {
	var url string
	if tag == "" {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPIBase, owner, repo)
	} else {
		url = fmt.Sprintf("%s/repos/%s/%s/releases/tags/%s", githubAPIBase, owner, repo, tag)
	}

	resp, err := httpClient.Get(url)
	if err != nil {
		return githubRelease{}, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return githubRelease{}, fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return githubRelease{}, fmt.Errorf("decoding release response: %w", err)
	}
	return release, nil
}

// downloadAsset fetches the asset at url and returns its bytes.
func downloadAsset(url string) ([]byte, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %d for %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	return data, nil
}

// findAsset returns the first asset whose name matches, or nil.
func findAsset(assets []githubAsset, name string) *githubAsset {
	for i := range assets {
		if assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

// verifyChecksum checks that the SHA256 of data matches the entry for filename
// in checksums.txt (format: "<hex>  <filename>" per line).
func verifyChecksum(checksumData []byte, filename string, data []byte) error {
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	for _, line := range strings.Split(string(checksumData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == filename {
			if fields[0] == hash {
				return nil
			}
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", filename, fields[0], hash)
		}
	}
	return fmt.Errorf("no checksum entry found for %s in checksums.txt", filename)
}

// validateManifest checks required fields and the executable naming convention.
func validateManifest(m manifestHeader) error {
	if m.Name == "" {
		return fmt.Errorf("manifest missing required field: name")
	}
	if m.Description == "" {
		return fmt.Errorf("manifest %q missing required field: description", m.Name)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest %q missing required field: version", m.Name)
	}
	if m.Executable == "" {
		return fmt.Errorf("manifest %q missing required field: executable", m.Name)
	}
	expected := "./bin/" + m.Name
	if m.Executable != expected {
		return fmt.Errorf("manifest %q: executable must be %q (got %q)", m.Name, expected, m.Executable)
	}
	return nil
}
