package sources

import (
	"os"
	"path/filepath"
	"testing"
)

// Every type in the registry parses and passes the checks, so a type
// that is published is one TeaNode can install.
func TestTheRegistryTypesParse(t *testing.T) {
	paths, err := filepath.Glob("testdata/registry/*.md")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no registry types to read: %v", err)
	}
	for _, path := range paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := Parse(content)
		if err != nil {
			t.Errorf("%s: %s", filepath.Base(path), err)
			continue
		}
		if parsed.Name+".md" != filepath.Base(path) {
			t.Errorf("%s is named %q", filepath.Base(path), parsed.Name)
		}
	}
}
