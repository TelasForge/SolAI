package tool

import (
	"os"
	"path/filepath"
)

// List reads every valid manifest directly under toolsDir. Directories without
// a readable manifest are skipped.
func List(toolsDir string) ([]Manifest, error) {
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := LoadManifest(filepath.Join(toolsDir, e.Name(), "manifest.json"))
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
