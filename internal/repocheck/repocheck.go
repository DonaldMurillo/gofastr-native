// Package repocheck finds things that should not ship in this repository:
// paths from one machine, names of the tools a change was made with,
// credentials, signing files, stale release claims, unmarked third-party
// copyright, and module layout mistakes.
//
// Its test runs as part of go test ./..., so every normal test run checks
// the tree. internal/releasecheck runs the same checks before a tag.
//
// A line that must keep a match (a test fixture, a documented example) can
// carry the marker "repocheck:allow" anywhere on the line.
package repocheck

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

// ModulePath is the root module's path.
const ModulePath = "github.com/DonaldMurillo/gofastr-native"

// ReleasedModules are the module directories that get version tags, relative
// to the repository root. Every other go.mod must live under examples/.
var ReleasedModules = []string{".", "tui"}

// AllowMarker exempts the line it is on from content rules.
const AllowMarker = "repocheck:allow"

// Finding is one problem in the tree.
type Finding struct {
	File string // slash-separated, relative to the repository root
	Line int    // 0 when the finding is about the whole file
	Rule string
	Text string // the matched text
	Hint string // how to fix it
}

func (f Finding) String() string {
	loc := f.File
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	if f.Text == "" {
		return fmt.Sprintf("%s: %s: %s", loc, f.Rule, f.Hint)
	}
	return fmt.Sprintf("%s: %s: %q: %s", loc, f.Rule, f.Text, f.Hint)
}

type rule struct {
	name string
	hint string
	re   *regexp.Regexp
	// applies reports whether the rule checks this file. Nil means every
	// text file.
	applies func(file string) bool
	// keep reports whether a match is a real finding. Nil keeps every match.
	keep func(groups []string) bool
}

func isMarkdown(file string) bool { return strings.EqualFold(path.Ext(file), ".md") }

// isAgentFile reports whether file holds instructions for coding agents,
// which may name the tools they are written for.
func isAgentFile(file string) bool {
	base := path.Base(file)
	return base == "AGENTS.md" || base == "CLAUDE.md" || strings.HasPrefix(file, ".claude/")
}

// placeholderUsers are directory names that stand for "your user" in docs
// and fixtures, not a real account.
var placeholderUsers = []string{"you", "your-name", "yourname", "user", "username", "me", "name", "someone", "example", "runner", "shared", "public"}

var exampleEmailDomains = []string{"example.com", "example.org", "example.net", "users.noreply.github.com", "noreply.github.com"}

var rules = []rule{
	{
		name: "home-directory",
		hint: "a path inside one person's home directory; use a relative path, ~, or a placeholder like /Users/you",
		re:   regexp.MustCompile(`(?:/Users/|/home/|\b[A-Za-z]:[\\/]{1,2}[Uu]sers[\\/]{1,2})([A-Za-z0-9._-]+)`),
		keep: func(g []string) bool { return !slices.Contains(placeholderUsers, strings.ToLower(g[1])) },
	},
	{
		name: "temp-directory",
		hint: "a temporary path from one run (mktemp suffix or macOS /var/folders); describe the step instead, e.g. GOCACHE=\"$(mktemp -d)\"",
		re:   regexp.MustCompile(`(?:/private)?/(?:tmp|var/folders/[^\s/]+/[^\s/]+/[TC])/[^\s` + "`" + `'"]*?\.([A-Za-z0-9]{6})\b|/var/folders/[A-Za-z0-9_+-]{2}/`),
		keep: func(g []string) bool {
			if g[1] == "" {
				return true // a /var/folders path
			}
			return looksRandom(g[1])
		},
	},
	{
		name:    "local-evidence-file",
		hint:    "points at a screenshot or log that exists only on one machine; say what was checked, or commit the file under testdata",
		re:      regexp.MustCompile(`(?:/private)?/tmp/[^\s` + "`" + `'")]+\.(?:png|jpe?g|gif|mov|mp4|webm|log|xcresult|trace)\b`),
		applies: isMarkdown,
	},
	{
		name:    "tool-name",
		hint:    "names the editor, agent or harness a change was made with; describe the step in tool-neutral words",
		re:      regexp.MustCompile(`(?i)\bT3(?: Code)?\b|\bcomputer[- ]use\b|\b(?:claude|chatgpt|openai|codex|copilot|gemini)\b`),
		applies: func(file string) bool { return !isAgentFile(file) },
	},
	{
		name:    "session-phrasing",
		hint:    "reads like a chat transcript; state the fact without the session that produced it",
		re:      regexp.MustCompile(`(?i)\bthis (?:continuation|conversation|chat)\b|\bthe user (?:asked|wants|requested|said)\b|\bas an AI\b|\bsubagents?\b`),
		applies: func(file string) bool { return isMarkdown(file) && !isAgentFile(file) },
	},
	{
		name: "email-address",
		hint: "a real email address; use an @example.com address",
		re:   regexp.MustCompile(`\b([A-Za-z0-9._%+-]+)@((?:[A-Za-z0-9-]+\.)+[A-Za-z]{2,})\b`),
		keep: func(g []string) bool {
			domain := strings.ToLower(g[2])
			if g[1] == "git" { // git@github.com:org/repo
				return false
			}
			for _, d := range exampleEmailDomains {
				if domain == d || strings.HasSuffix(domain, "."+d) {
					return false
				}
			}
			for _, tld := range []string{".example", ".test", ".invalid", ".localhost"} {
				if strings.HasSuffix(domain, tld) {
					return false
				}
			}
			return true
		},
	},
	{
		name: "credential",
		hint: "looks like a private key or access token; remove it and rotate the credential",
		re:   regexp.MustCompile(`-----BEGIN (?:[A-Z]+ )*PRIVATE KEY-----|\bAKIA[0-9A-Z]{16}\b|\bgh[pousr]_[A-Za-z0-9]{36,}\b|\bgithub_pat_[A-Za-z0-9_]{40,}\b|\bsk-(?:ant-)?[A-Za-z0-9_-]{24,}\b|\bxox[abprs]-[A-Za-z0-9-]{10,}\b`),
	},
	{
		name: "stale-release-claim",
		hint: "gofastr-native has published versions; describe how to use one",
		re:   regexp.MustCompile(`(?i)\bno published (?:version|release)s? yet\b|\buntil gofastr-native publishes\b`),
	},
}

// looksRandom reports whether a six-character suffix looks like mktemp
// output (IUXdIL, a8Fk2q) rather than a word (Server, backup).
func looksRandom(s string) bool {
	var upper, lower, digit int
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		case r >= '0' && r <= '9':
			digit++
		}
	}
	letters := upper + lower
	return upper > 0 && lower > 0 && (upper >= 2 || digit > 0) || digit > 0 && letters > 0
}

// signingExtensions are files that hold keys or provisioning data and never
// belong in the repository.
var signingExtensions = []string{".jks", ".keystore", ".p12", ".pfx", ".p8", ".mobileprovision", ".provisionprofile", ".pem", ".key"}

// skipContent lists files whose content is not checked: lock files and this
// package, whose rules and fixtures match themselves.
func skipContent(file string) bool {
	base := path.Base(file)
	return base == "go.sum" || base == "go.work.sum" || strings.HasPrefix(file, "internal/repocheck/")
}

// Files lists the files a commit would include: tracked files plus untracked
// files that are not ignored. Without git it walks the tree.
func Files(root string) ([]string, error) {
	cmd := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	cmd.Dir = root
	if out, err := cmd.Output(); err == nil {
		var files []string
		for name := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
			if name == "" {
				continue
			}
			if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				continue // deleted in the working tree
			}
			files = append(files, name)
		}
		slices.Sort(files)
		files = slices.Compact(files)
		return files, nil
	}
	var files []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == "dist" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			files = append(files, rel)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

// Check runs every check on the tree at root.
func Check(root string) ([]Finding, error) {
	files, err := Files(root)
	if err != nil {
		return nil, err
	}
	var findings []Finding
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			return nil, err
		}
		findings = append(findings, CheckFile(file, data)...)
	}
	findings = append(findings, checkLayout(root, files)...)
	findings = append(findings, checkThirdParty(root, files)...)
	return findings, nil
}

// CheckFile runs the per-file rules on one file's content.
func CheckFile(file string, data []byte) []Finding {
	var findings []Finding
	ext := strings.ToLower(path.Ext(file))
	if slices.Contains(signingExtensions, ext) {
		findings = append(findings, Finding{File: file, Rule: "signing-file", Hint: "keys and provisioning profiles stay outside the repository"})
	}
	if base := path.Base(file); base == ".env" || strings.HasPrefix(base, ".env.") {
		findings = append(findings, Finding{File: file, Rule: "signing-file", Hint: "environment files hold secrets; keep them untracked"})
	}
	if skipContent(file) || isBinary(data) {
		return findings
	}
	for n, line := range bytes.Split(data, []byte("\n")) {
		if bytes.Contains(line, []byte(AllowMarker)) {
			continue
		}
		for _, r := range rules {
			if r.applies != nil && !r.applies(file) {
				continue
			}
			for _, m := range r.re.FindAllSubmatch(line, -1) {
				groups := make([]string, len(m))
				for i := range m {
					groups[i] = string(m[i])
				}
				if r.keep != nil && !r.keep(groups) {
					continue
				}
				findings = append(findings, Finding{File: file, Line: n + 1, Rule: r.name, Text: groups[0], Hint: r.hint})
			}
		}
	}
	return findings
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0
}

// checkLayout checks the files a release depends on and the module layout.
func checkLayout(root string, files []string) []Finding {
	var findings []Finding
	has := func(f string) bool { _, ok := slices.BinarySearch(files, f); return ok }

	for _, f := range []string{"LICENSE", "README.md", "CHANGELOG.md"} {
		if !has(f) {
			findings = append(findings, Finding{File: f, Rule: "release-file", Hint: "missing; every release ships it"})
		}
	}
	if readme, err := os.ReadFile(filepath.Join(root, "README.md")); err == nil && !regexp.MustCompile(`(?m)^## License\b`).Match(readme) {
		findings = append(findings, Finding{File: "README.md", Rule: "release-file", Hint: "needs a ## License section"})
	}
	for _, f := range []string{"go.work", "go.work.sum"} {
		if has(f) {
			findings = append(findings, Finding{File: f, Rule: "local-workspace", Hint: "go.work points at local checkouts; keep it untracked (see go.work.example)"})
		}
	}

	for _, f := range files {
		if path.Base(f) != "go.mod" {
			continue
		}
		dir := path.Dir(f)
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		mf, err := modfile.Parse(f, data, nil)
		if err != nil || mf.Module == nil {
			findings = append(findings, Finding{File: f, Rule: "module-layout", Hint: "cannot parse go.mod"})
			continue
		}
		want := ModulePath
		if dir != "." {
			want = ModulePath + "/" + dir
		}
		if mf.Module.Mod.Path != want {
			findings = append(findings, Finding{File: f, Rule: "module-layout", Text: mf.Module.Mod.Path, Hint: "module path must be " + want + " to match its directory"})
		}
		released := slices.Contains(ReleasedModules, dir)
		if !released && !strings.HasPrefix(dir, "examples/") {
			findings = append(findings, Finding{File: f, Rule: "module-layout", Hint: "a new module outside examples/; add it to repocheck.ReleasedModules so it gets tagged, or move it under examples/"})
		}
		if released {
			for _, r := range mf.Replace {
				findings = append(findings, Finding{File: f, Line: r.Syntax.Start.Line, Rule: "released-replace", Text: r.Old.Path, Hint: "a released module cannot carry a replace: go install and go run @version refuse it"})
			}
		}
	}
	return findings
}

var copyrightLine = regexp.MustCompile(`(?i)(?:\bcopyright\b|©)\s*(?:\(c\)\s*)?(?:©\s*)?\d{4}|SPDX-License-Identifier`)

// checkThirdParty requires every file that carries a copyright notice other
// than the project's own LICENSE to be credited in the README's License
// section, by its path or one of its parent directories.
func checkThirdParty(root string, files []string) []Finding {
	readme, _ := os.ReadFile(filepath.Join(root, "README.md"))
	section := licenseSection(string(readme))
	var findings []Finding
	for _, f := range files {
		if f == "LICENSE" || skipContent(f) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil || isBinary(data) {
			continue
		}
		head := data[:min(len(data), 4000)]
		loc := copyrightLine.FindIndex(head)
		if loc == nil || bytes.Contains(lineAt(head, loc[0]), []byte(AllowMarker)) {
			continue
		}
		if credited(section, f) {
			continue
		}
		findings = append(findings, Finding{File: f, Line: bytes.Count(head[:loc[0]], []byte("\n")) + 1, Rule: "third-party-notice", Text: string(head[loc[0]:loc[1]]),
			Hint: "carries someone else's copyright; check its license allows redistribution and credit its path in README.md's License section"})
	}
	return findings
}

func lineAt(data []byte, i int) []byte {
	start := bytes.LastIndexByte(data[:i], '\n') + 1
	end := bytes.IndexByte(data[i:], '\n')
	if end < 0 {
		return data[start:]
	}
	return data[start : i+end]
}

func licenseSection(readme string) string {
	_, after, ok := strings.Cut(readme, "\n## License")
	if !ok {
		return ""
	}
	if next := strings.Index(after, "\n## "); next >= 0 {
		after = after[:next]
	}
	return after
}

func credited(section, file string) bool {
	for p := file; p != "." && p != "/"; p = path.Dir(p) {
		if strings.Contains(section, p) {
			return true
		}
	}
	return false
}
