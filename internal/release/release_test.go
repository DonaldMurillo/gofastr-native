package main

import (
	"slices"
	"strings"
	"testing"
)

const changelog = `# Changelog

Intro.

## v0.2.0 (2026-12-01)

### Phone apps

- Added a thing.

## v0.1.0 (2026-11-20)

- First.
`

func TestChangelogSection(t *testing.T) {
	got := changelogSection(changelog, "v0.2.0")
	if want := "### Phone apps\n\n- Added a thing.\n"; got != want {
		t.Errorf("v0.2.0 section = %q, want %q", got, want)
	}
	if got := changelogSection(changelog, "v0.1.0"); got != "- First.\n" {
		t.Errorf("last section = %q", got)
	}
	if got := changelogSection(changelog, "v0.3.0"); got != "" {
		t.Errorf("missing section = %q, want empty", got)
	}
	if got := changelogSection(changelog, "v0.2"); got != "" {
		t.Errorf("prefix version matched: %q", got)
	}
}

func TestTagNamesAndModules(t *testing.T) {
	if got, want := tagNames("v1.2.3"), []string{"v1.2.3", "tui/v1.2.3"}; !slices.Equal(got, want) {
		t.Errorf("tagNames = %v, want %v", got, want)
	}
	for _, p := range modulePaths() {
		if !strings.HasPrefix(p, "github.com/DonaldMurillo/gofastr-native") {
			t.Errorf("module path %s", p)
		}
	}
}

func TestGhReleaseArgs(t *testing.T) {
	if got := ghReleaseArgs("v1.0.0", "notes.md"); !slices.Contains(got, "--verify-tag") || slices.Contains(got, "--prerelease") {
		t.Errorf("release args = %v", got)
	}
	if got := ghReleaseArgs("v1.0.0-rc.1", "notes.md"); !slices.Contains(got, "--prerelease") {
		t.Errorf("prerelease args = %v", got)
	}
}

func TestConfirm(t *testing.T) {
	var out strings.Builder
	if !confirm(&out, strings.NewReader("v0.0.1\n"), "v0.0.1") {
		t.Error("exact version was refused")
	}
	for _, answer := range []string{"y\n", "", "v0.0.2\n"} {
		if confirm(&out, strings.NewReader(answer), "v0.0.1") {
			t.Errorf("%q confirmed", answer)
		}
	}
}
