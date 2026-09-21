package agent

import "testing"

// A project is "written in" languages, not file extensions. The first
// version named the three commonest extensions, and told the person their
// main project was written mostly in gitignore.
func TestLanguagesOfNamesLanguagesNotExtensions(t *testing.T) {
	got := languagesOf(map[string]int{
		"gitignore": 40, "modules": 30, "jon": 25, // the tooling that won before
		"go": 300, "ts": 120, "tsx": 80, "md": 50, "json": 45, "py": 3,
	})
	if got != "Go, TypeScript" {
		t.Fatalf("Go and TypeScript, by file count, with the rest dropped: %q", got)
	}
	// Nothing recognisable is nothing, not the least-bad extension.
	if got := languagesOf(map[string]int{"gitignore": 3, "lock": 2}); got != "" {
		t.Fatalf("a tree with no source in it is written in nothing, not %q", got)
	}
}
