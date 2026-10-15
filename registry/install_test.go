package registry

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// ---- parseRef ---------------------------------------------------------------

func TestParseRef_OwnerRepo(t *testing.T) {
	owner, repo, tag := parseRef("alice/my-tool")
	if owner != "alice" {
		t.Errorf("owner: got %q, want %q", owner, "alice")
	}
	if repo != "my-tool" {
		t.Errorf("repo: got %q, want %q", repo, "my-tool")
	}
	if tag != "" {
		t.Errorf("tag: got %q, want empty", tag)
	}
}

func TestParseRef_WithTag(t *testing.T) {
	owner, repo, tag := parseRef("alice/my-tool@v1.2.3")
	if owner != "alice" {
		t.Errorf("owner: got %q, want %q", owner, "alice")
	}
	if repo != "my-tool" {
		t.Errorf("repo: got %q, want %q", repo, "my-tool")
	}
	if tag != "v1.2.3" {
		t.Errorf("tag: got %q, want %q", tag, "v1.2.3")
	}
}

func TestParseRef_MissingSlash(t *testing.T) {
	owner, repo, _ := parseRef("justowner")
	if owner != "" || repo != "" {
		t.Errorf("expected empty owner/repo for invalid ref, got %q/%q", owner, repo)
	}
}

func TestParseRef_Empty(t *testing.T) {
	owner, repo, tag := parseRef("")
	if owner != "" || repo != "" || tag != "" {
		t.Errorf("expected all empty, got %q/%q@%q", owner, repo, tag)
	}
}

func TestParseRef_AtSignInTag(t *testing.T) {
	// Only the first @ is used as separator.
	_, _, tag := parseRef("alice/repo@v1@extra")
	if tag != "v1@extra" {
		t.Errorf("tag: got %q, want %q", tag, "v1@extra")
	}
}

// ---- findAsset --------------------------------------------------------------

func TestFindAsset_Found(t *testing.T) {
	assets := []githubAsset{
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		{Name: "mytool-linux-amd64", BrowserDownloadURL: "https://example.com/mytool-linux-amd64"},
	}
	got := findAsset(assets, "mytool-linux-amd64")
	if got == nil {
		t.Fatal("expected to find asset, got nil")
	}
	if got.Name != "mytool-linux-amd64" {
		t.Errorf("got asset name %q", got.Name)
	}
}

func TestFindAsset_NotFound(t *testing.T) {
	assets := []githubAsset{
		{Name: "other-file.txt"},
	}
	if got := findAsset(assets, "missing"); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestFindAsset_EmptySlice(t *testing.T) {
	if got := findAsset(nil, "anything"); got != nil {
		t.Errorf("expected nil for empty slice, got %+v", got)
	}
}

func TestFindAsset_ReturnsFirstMatch(t *testing.T) {
	assets := []githubAsset{
		{Name: "dup", BrowserDownloadURL: "https://first.example.com"},
		{Name: "dup", BrowserDownloadURL: "https://second.example.com"},
	}
	got := findAsset(assets, "dup")
	if got.BrowserDownloadURL != "https://first.example.com" {
		t.Errorf("expected first match, got URL %q", got.BrowserDownloadURL)
	}
}

// ---- verifyChecksum ---------------------------------------------------------

func checksumLine(filename string, data []byte) []byte {
	h := sha256.Sum256(data)
	return []byte(fmt.Sprintf("%x  %s\n", h, filename))
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello binary")
	cs := checksumLine("mybinary", data)
	if err := verifyChecksum(cs, "mybinary", data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("hello binary")
	cs := checksumLine("mybinary", []byte("different content"))
	err := verifyChecksum(cs, "mybinary", data)
	if err == nil {
		t.Fatal("expected checksum mismatch error, got nil")
	}
}

func TestVerifyChecksum_MissingEntry(t *testing.T) {
	data := []byte("hello binary")
	cs := checksumLine("other-file", data)
	err := verifyChecksum(cs, "mybinary", data)
	if err == nil {
		t.Fatal("expected error for missing entry, got nil")
	}
}

func TestVerifyChecksum_EmptyChecksums(t *testing.T) {
	err := verifyChecksum([]byte(""), "mybinary", []byte("data"))
	if err == nil {
		t.Fatal("expected error for empty checksums, got nil")
	}
}

func TestVerifyChecksum_MultipleEntries(t *testing.T) {
	data := []byte("target content")
	other := []byte("other content")
	csOther := checksumLine("other", other)
	csTarget := checksumLine("target", data)
	combined := append(csOther, csTarget...)
	if err := verifyChecksum(combined, "target", data); err != nil {
		t.Fatalf("unexpected error with multiple entries: %v", err)
	}
}

// ---- validateManifest -------------------------------------------------------

func TestValidateManifest_Valid(t *testing.T) {
	m := manifestHeader{
		Name:        "my-tool",
		Description: "does stuff",
		Version:     "1.0.0",
		Executable:  "./bin/my-tool",
	}
	if err := validateManifest(m); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateManifest_MissingName(t *testing.T) {
	m := manifestHeader{Description: "d", Executable: "./bin/foo"}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateManifest_MissingDescription(t *testing.T) {
	m := manifestHeader{Name: "foo", Executable: "./bin/foo"}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for missing description")
	}
}

func TestValidateManifest_MissingExecutable(t *testing.T) {
	m := manifestHeader{Name: "foo", Description: "d"}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error for missing executable")
	}
}

func TestValidateManifest_WrongExecutablePath(t *testing.T) {
	m := manifestHeader{
		Name:        "my-tool",
		Description: "d",
		Executable:  "./my-tool", // must be "./bin/my-tool"
	}
	err := validateManifest(m)
	if err == nil {
		t.Fatal("expected error for wrong executable path")
	}
}

func TestValidateManifest_ExecutableMustMatchName(t *testing.T) {
	m := manifestHeader{
		Name:        "foo",
		Description: "d",
		Executable:  "./bin/bar", // name mismatch
	}
	if err := validateManifest(m); err == nil {
		t.Fatal("expected error when executable doesn't match name")
	}
}

// ---- Install (httptest) -----------------------------------------------------

func makeTestServer(t *testing.T, toolName string) *httptest.Server {
	t.Helper()

	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("%s-linux-%s", toolName, arch)
	binaryContent := []byte("fake binary content")

	manifest := manifestHeader{
		Name:        toolName,
		Description: "A test tool",
		Version:     "1.0.0",
		Executable:  "./bin/" + toolName,
	}
	manifestJSON, _ := json.Marshal(manifest)

	checksumContent := checksumLine(binaryName, binaryContent)

	release := githubRelease{
		TagName: "v1.0.0",
		Assets: []githubAsset{
			{Name: "manifest.json", BrowserDownloadURL: "PLACEHOLDER/manifest.json"},
			{Name: binaryName, BrowserDownloadURL: "PLACEHOLDER/" + binaryName},
			{Name: "checksums.txt", BrowserDownloadURL: "PLACEHOLDER/checksums.txt"},
		},
	}

	mux := http.NewServeMux()
	var srv *httptest.Server

	// We need to set up a server first, then fix URLs in the release JSON.
	// Use a closure that captures srv after Start().

	files := map[string][]byte{
		"/manifest.json": manifestJSON,
		"/" + binaryName: binaryContent,
		"/checksums.txt": checksumContent,
	}

	mux.HandleFunc("/repos/owner/"+toolName+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		// Fix asset URLs to point to our test server.
		adjusted := release
		assets := make([]githubAsset, len(adjusted.Assets))
		for i, a := range adjusted.Assets {
			a.BrowserDownloadURL = srv.URL + "/" + a.Name
			assets[i] = a
		}
		adjusted.Assets = assets
		json.NewEncoder(w).Encode(adjusted)
	})

	for path, content := range files {
		content := content // capture
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Write(content)
		})
	}

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestInstall_Success(t *testing.T) {
	const toolName = "test-tool"
	srv := makeTestServer(t, toolName)

	// Override the API base to our test server.
	original := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = original })

	toolsDir := t.TempDir()
	if err := Install("owner/"+toolName, toolsDir); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Verify manifest was written.
	manifestPath := filepath.Join(toolsDir, toolName, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest.json not found: %v", err)
	}

	// Verify binary was written and is executable.
	binaryPath := filepath.Join(toolsDir, toolName, "bin", toolName)
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("binary not executable: mode %v", info.Mode())
	}
}

func TestInstall_InvalidOwnerRepo(t *testing.T) {
	// An owner/repo ref with no repo part should fail immediately without network.
	if err := Install("/missingowner", t.TempDir()); err == nil {
		t.Fatal("expected error for ref missing owner")
	}
}

func TestInstall_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	original := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = original })

	if err := Install("owner/repo", t.TempDir()); err == nil {
		t.Fatal("expected error when API returns 404")
	}
}

func TestInstall_NoManifestAsset(t *testing.T) {
	release := githubRelease{TagName: "v1.0.0", Assets: []githubAsset{}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	t.Cleanup(srv.Close)

	original := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = original })

	if err := Install("owner/repo", t.TempDir()); err == nil {
		t.Fatal("expected error when no manifest.json asset")
	}
}

func TestInstall_AlreadyInstalled_GitHub(t *testing.T) {
	const toolName = "test-tool"
	srv := makeTestServer(t, toolName)

	original := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = original })

	toolsDir := t.TempDir()
	if err := Install("owner/"+toolName, toolsDir); err != nil {
		t.Fatalf("first install: %v", err)
	}
	err := Install("owner/"+toolName, toolsDir)
	if err == nil {
		t.Fatal("expected error on second install, got nil")
	}
}

// ---- validateToolName -------------------------------------------------------

func TestValidateToolName_Valid(t *testing.T) {
	for _, name := range []string{"token-price", "my-tool", "birdeye", "tool123", "a"} {
		if err := validateToolName(name); err != nil {
			t.Errorf("expected %q to be valid, got: %v", name, err)
		}
	}
}

func TestValidateToolName_Invalid(t *testing.T) {
	for _, name := range []string{
		"",
		".",
		"..",
		"../evil",
		"/absolute",
		"has/slash",
		"has space",
		"UPPER",
		"-starts-with-hyphen",
		"has.dot",
	} {
		if err := validateToolName(name); err == nil {
			t.Errorf("expected %q to be invalid, got nil error", name)
		}
	}
}

// ---- parseShortRef ----------------------------------------------------------

func TestParseShortRef_NameOnly(t *testing.T) {
	name, tag := parseShortRef("token-price")
	if name != "token-price" {
		t.Errorf("name: got %q, want %q", name, "token-price")
	}
	if tag != "" {
		t.Errorf("tag: got %q, want empty", tag)
	}
}

func TestParseShortRef_WithTag(t *testing.T) {
	name, tag := parseShortRef("token-price@v1.2.3")
	if name != "token-price" {
		t.Errorf("name: got %q, want %q", name, "token-price")
	}
	if tag != "v1.2.3" {
		t.Errorf("tag: got %q, want %q", tag, "v1.2.3")
	}
}

// ---- IsInstalled ------------------------------------------------------------

func TestIsInstalled_False(t *testing.T) {
	if IsInstalled("no-such-tool", t.TempDir()) {
		t.Error("expected false for non-existent tool")
	}
}

func TestIsInstalled_True(t *testing.T) {
	toolsDir := t.TempDir()
	toolDir := filepath.Join(toolsDir, "my-tool", "bin")
	if err := os.MkdirAll(toolDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolsDir, "my-tool", "manifest.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !IsInstalled("my-tool", toolsDir) {
		t.Error("expected true for installed tool")
	}
}

// ---- installFromIndex -------------------------------------------------------

// makeIndexServer returns a test server that serves the registry index and tool assets.
func makeIndexServer(t *testing.T, toolName string) *httptest.Server {
	t.Helper()

	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("%s-linux-%s", toolName, arch)
	binaryContent := []byte("fake binary content")
	manifest := manifestHeader{
		Name:        toolName,
		Description: "A test tool",
		Version:     "1.0.0",
		Executable:  "./bin/" + toolName,
	}
	manifestJSON, _ := json.Marshal(manifest)
	checksumContent := checksumLine(binaryName, binaryContent)

	mux := http.NewServeMux()
	var srv *httptest.Server

	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(manifestJSON)
	})
	mux.HandleFunc("/"+binaryName, func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	})
	mux.HandleFunc("/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Write(checksumContent)
	})
	mux.HandleFunc("/index.json", func(w http.ResponseWriter, r *http.Request) {
		idx := indexFile{
			Tools: map[string]indexEntry{
				toolName: {
					Latest: "v1.0.0",
					Versions: map[string]indexVersion{
						"v1.0.0": {
							Manifest:  srv.URL + "/manifest.json",
							Checksums: srv.URL + "/checksums.txt",
							Assets: map[string]string{
								"linux/" + arch: srv.URL + "/" + binaryName,
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(idx)
	})

	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestInstallFromIndex_Success(t *testing.T) {
	const toolName = "idx-tool"
	srv := makeIndexServer(t, toolName)

	origIndex := indexURL
	indexURL = srv.URL + "/index.json"
	t.Cleanup(func() { indexURL = origIndex })

	toolsDir := t.TempDir()
	if err := Install(toolName, toolsDir); err != nil {
		t.Fatalf("Install: %v", err)
	}

	manifestPath := filepath.Join(toolsDir, toolName, "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Errorf("manifest.json not found: %v", err)
	}
	binaryPath := filepath.Join(toolsDir, toolName, "bin", toolName)
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("binary not found: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("binary not executable: mode %v", info.Mode())
	}
}

func TestInstallFromIndex_WithTag(t *testing.T) {
	const toolName = "idx-tool"
	srv := makeIndexServer(t, toolName)

	origIndex := indexURL
	indexURL = srv.URL + "/index.json"
	t.Cleanup(func() { indexURL = origIndex })

	toolsDir := t.TempDir()
	if err := Install(toolName+"@v1.0.0", toolsDir); err != nil {
		t.Fatalf("Install with tag: %v", err)
	}
}

func TestInstallFromIndex_ToolNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(indexFile{Tools: map[string]indexEntry{}})
	}))
	t.Cleanup(srv.Close)

	origIndex := indexURL
	indexURL = srv.URL
	t.Cleanup(func() { indexURL = origIndex })

	if err := Install("nonexistent", t.TempDir()); err == nil {
		t.Fatal("expected error for tool not in index")
	}
}

func TestInstallFromIndex_UnknownVersion(t *testing.T) {
	const toolName = "idx-tool"
	srv := makeIndexServer(t, toolName)

	origIndex := indexURL
	indexURL = srv.URL + "/index.json"
	t.Cleanup(func() { indexURL = origIndex })

	if err := Install(toolName+"@v99.0.0", t.TempDir()); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestInstallFromIndex_AlreadyInstalled(t *testing.T) {
	const toolName = "idx-tool"
	srv := makeIndexServer(t, toolName)

	origIndex := indexURL
	indexURL = srv.URL + "/index.json"
	t.Cleanup(func() { indexURL = origIndex })

	toolsDir := t.TempDir()
	if err := Install(toolName, toolsDir); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := Install(toolName, toolsDir); err == nil {
		t.Fatal("expected error on second install, got nil")
	}
}
