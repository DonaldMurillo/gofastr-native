package repocheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTreeIsClean is the check that runs with every go test ./...: it fails
// on anything in the working tree a commit would carry that should not ship.
func TestTreeIsClean(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	findings, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	if len(findings) > 0 {
		t.Logf("fix each finding, or add %q to a line that must keep its match (a fixture or a placeholder)", AllowMarker)
	}
}

func TestRules(t *testing.T) {
	// The fixtures are split so this file does not match its own rules.
	home := "/Us" + "ers/"
	cases := []struct {
		file, line, want string // want "" means no finding
	}{
		{"docs/a.md", "cd " + home + "alex/src/app", "home-directory"},
		{"docs/a.md", "cd " + home + "you/src/app", ""},
		{"a.go", `x := "C:\\Us` + `ers\\alex\\data"`, "home-directory"},
		{"a.go", `x := "C:\Us` + `ers\someone\data"`, ""},
		{"a.go", "/ho" + "me/sam/.config", "home-directory"},
		{"docs/a.md", "GOCACHE=/t" + "mp/build-cache.IUXdIL go test", "temp-directory"},
		{"docs/a.md", `GOCACHE="$(mktemp -d /t` + `mp/build-cache.XXXXXX)"`, ""},
		{"docs/a.md", "/t" + "mp/notes.backup", ""},
		{"docs/a.md", "see /var/fol" + "ders/zz/abc123/T/x", "temp-directory"},
		{"docs/a.md", "screenshot at `/t" + "mp/run/shot.png`", "local-evidence-file"},
		{"a.go", "// /t" + "mp/run/shot.png", ""},
		{"docs/a.md", "SIM_SHOTS=/t" + "mp/shots go test", ""},
		{"docs/a.md", "checked in the T" + "3 Code device panel", "tool-name"},
		{"docs/a.md", "computer" + "-use failed", "tool-name"},
		{"docs/a.md", "asked Cl" + "aude to fix it", "tool-name"},
		{"AGENTS.md", "Cl" + "aude reads this file", ""},
		{".claude/skills/release/SKILL.md", "Cl" + "aude skill", ""},
		{"docs/a.md", "not changed in this contin" + "uation", "session-phrasing"},
		{"a.go", `"draft kept for this session"`, ""},
		{"docs/a.md", "mail dev" + "@corp.io", "email-address"},
		{"a.go", "a" + "@example.com", ""},
		{"docs/a.md", "git" + "@github.com:org/repo.git", ""},
		{"a.go", "-----BEGIN RSA PRI" + "VATE KEY-----", "credential"},
		{"a.go", "token gh" + "p_" + strings.Repeat("a1", 18), "credential"},
		{"docs/a.md", "gofastr-native has no publi" + "shed version yet", "stale-release-claim"},
		{"docs/a.md", "cd " + home + "alex/x " + AllowMarker, ""},
	}
	for _, c := range cases {
		got := CheckFile(c.file, []byte(c.line))
		switch {
		case c.want == "" && len(got) > 0:
			t.Errorf("%s %q: unexpected %v", c.file, c.line, got)
		case c.want != "" && (len(got) == 0 || got[0].Rule != c.want):
			t.Errorf("%s %q: want %s, got %v", c.file, c.line, c.want, got)
		}
	}
}

func TestSigningFiles(t *testing.T) {
	for _, f := range []string{"keys/upload.p12", "ios/dev.mobileprovision", "android/release.jks", ".env", "app/.env.local"} {
		if got := CheckFile(f, []byte("x")); len(got) == 0 || got[0].Rule != "signing-file" {
			t.Errorf("%s: want signing-file, got %v", f, got)
		}
	}
	if got := CheckFile("docs/keys.md", []byte("x")); len(got) != 0 {
		t.Errorf("docs/keys.md: unexpected %v", got)
	}
}

func TestLayout(t *testing.T) {
	root := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("LICENSE", "MIT")
	write("README.md", "# x\n\n## License\n\nMIT. Data in `vendored/data` is used under its own terms.\n")
	write("CHANGELOG.md", "# Changelog\n")
	write("go.mod", "module "+ModulePath+"\n\ngo 1.24\n")
	write("tui/go.mod", "module "+ModulePath+"/tui\n\ngo 1.24\n\nreplace example.com/x => ../x\n")
	write("examples/demo/go.mod", "module "+ModulePath+"/examples/demo\n\ngo 1.24\n\nreplace "+ModulePath+" => ../..\n")
	write("tools/go.mod", "module "+ModulePath+"/tools\n\ngo 1.24\n")
	write("examples/wrong/go.mod", "module example.com/wrong\n\ngo 1.24\n")
	write("vendored/data/table.txt", "# Copyright 2020 Someone Else\n")
	write("third/font.txt", "Copyright (c) 2019 Font Maker\n")

	findings, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range findings {
		got[f.File+" "+f.Rule] = true
	}
	for _, want := range []string{
		"tui/go.mod released-replace",
		"tools/go.mod module-layout",
		"examples/wrong/go.mod module-layout",
		"third/font.txt third-party-notice",
	} {
		if !got[want] {
			t.Errorf("missing %s in %v", want, findings)
		}
	}
	for _, unwanted := range []string{"examples/demo/go.mod released-replace", "vendored/data/table.txt third-party-notice", "README.md release-file"} {
		if got[unwanted] {
			t.Errorf("unexpected %s", unwanted)
		}
	}
}

func TestClassifyLicense(t *testing.T) {
	cases := map[string]string{
		"MIT License\n\nPermission is hereby granted, free of charge, to any person":                                     "MIT",
		"Apache License\n Version 2.0, January 2004":                                                                     "Apache-2.0",
		"Redistribution and use in source and binary forms ... Neither the name of Google":                               "BSD-3-Clause",
		"Redistribution and use in source and binary forms, with or without modification":                                "BSD-2-Clause",
		"Permission to use, copy, modify, and/or distribute this software for any purpose with or without fee is hereby": "ISC",
		"GNU GENERAL PUBLIC LICENSE Version 3":                                                                           "GPL",
		"GNU LESSER GENERAL PUBLIC LICENSE":                                                                              "LGPL",
		"Mozilla Public License Version 2.0":                                                                             "MPL-2.0",
		"All rights reserved.":                                                                                           "unknown",
	}
	for text, want := range cases {
		if got := ClassifyLicense(text); got != want {
			t.Errorf("ClassifyLicense(%.40q) = %s, want %s", text, got, want)
		}
	}
}
