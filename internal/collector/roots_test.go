package collector

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestExpandRoots(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{".claude-b/projects", ".claude-a/projects", ".claude-file"} {
		p := filepath.Join(base, d)
		if filepath.Base(d) == ".claude-file" {
			if err := os.WriteFile(p, nil, 0o600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	missing := filepath.Join(base, "missing")
	got := ExpandRoots([]string{
		missing,
		filepath.Join(base, ".claude-*", "projects"),
		filepath.Join(base, ".claude-a", "projects") + string(filepath.Separator), // same dir again
		filepath.Join(base, ".claude-*"),                                          // the file is not a dir
		"",
	})
	want := []string{
		missing,
		filepath.Join(base, ".claude-a", "projects"),
		filepath.Join(base, ".claude-b", "projects"),
		filepath.Join(base, ".claude-a"),
		filepath.Join(base, ".claude-b"),
	}
	if !slices.Equal(got, want) {
		t.Errorf("ExpandRoots =\n %v\nwant\n %v", got, want)
	}
}

func TestExpandRootsLiteralBracketFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "[team]", "projects")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := ExpandRoots([]string{dir}); !slices.Equal(got, []string{dir}) {
		t.Errorf("a folder named with [ ] was dropped: %v", got)
	}
}
