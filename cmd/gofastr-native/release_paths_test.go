package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReleaseInputContainment(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"key.jks", ".keys/upload.jks", "..keys/upload.jks"} {
		if !inside(dir, filepath.Join(dir, filepath.FromSlash(name))) {
			t.Errorf("input inside output was allowed: %s", name)
		}
	}
	if inside(dir, filepath.Join(filepath.Dir(dir), "outside.jks")) {
		t.Fatal("input outside output was classified as inside")
	}
}

func TestReleaseInputContainmentResolvesLinks(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "output")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(out, "upload.jks")
	if err := os.WriteFile(key, []byte("test fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.jks")
	if err := os.Symlink(key, link); err != nil {
		t.Skipf("file symlinks unavailable: %v", err)
	}
	if !inside(out, link) {
		t.Fatal("linked signing input inside output was allowed")
	}
}

func TestReleaseInputContainmentWithMissingPathsAndAliases(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "output")
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("directory symlinks unavailable: %v", err)
	}
	outside := filepath.Join(root, "outside.jks")
	if err := os.WriteFile(outside, []byte("test fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	outwardLink := filepath.Join(out, "outward.jks")
	if err := os.Symlink(outside, outwardLink); err != nil {
		t.Fatal(err)
	}
	danglingLink := filepath.Join(root, "dangling.jks")
	if err := os.Symlink(filepath.Join(out, "missing.jks"), danglingLink); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, dir, input string
		want             bool
	}{
		{"missing input via alias", out, filepath.Join(alias, "output", ".keys", "upload.jks"), true},
		{"output via alias", filepath.Join(alias, "output"), filepath.Join(out, "..keys", "upload.jks"), true},
		{"missing output", filepath.Join(alias, "new", "output"), filepath.Join(root, "new", "output", "key.jks"), true},
		{"lexically inside with outward link", out, outwardLink, true},
		{"dangling input fails closed", out, danglingLink, true},
		{"outside via alias", out, filepath.Join(alias, "outside.jks"), false},
		{"missing outside via alias", out, filepath.Join(alias, "other", "key.jks"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inside(tc.dir, tc.input); got != tc.want {
				t.Fatalf("inside(%q, %q) = %v, want %v", tc.dir, tc.input, got, tc.want)
			}
		})
	}
}
