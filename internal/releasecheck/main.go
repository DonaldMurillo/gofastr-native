// Command releasecheck checks that the repository is ready to tag a version.
// It changes nothing: it never tags, pushes or edits files.
//
//	go run ./internal/releasecheck v0.0.2
//	go run ./internal/releasecheck -skip-tests v0.0.2   # everything but go test and node --test
//
// It checks, in order: the version and CHANGELOG entry, a clean working tree
// and unused tags, the repocheck hygiene rules, then a fresh clone of HEAD
// with GOWORK=off (tidy, gofmt, vet, tests, the WebView Node tests), and the
// license of every dependency module that is built or tested. It ends by
// printing the tag commands. See RELEASING.md.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/DonaldMurillo/gofastr-native/internal/repocheck"
)

type status string

const (
	pass   status = "PASS"
	fail   status = "FAIL"
	notRun status = "NOT RUN"
)

type checker struct {
	out     io.Writer
	results []string
	failed  bool
}

func (c *checker) record(s status, name, detail string) {
	line := fmt.Sprintf("%-7s %s", s, name)
	if detail != "" {
		line += ": " + detail
	}
	fmt.Fprintln(c.out, line)
	c.results = append(c.results, line)
	if s == fail {
		c.failed = true
	}
}

func main() {
	skipTests := flag.Bool("skip-tests", false, "skip go test and node --test (tidy, gofmt, vet and licenses still run)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/releasecheck [-skip-tests] vX.Y.Z")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := &checker{out: os.Stdout}
	run(c, root, flag.Arg(0), *skipTests)
	fmt.Println()
	if c.failed {
		fmt.Println("Not ready: fix every FAIL above, commit, and run this again.")
		os.Exit(1)
	}
	fmt.Println("Ready. Review the NOT RUN lines, then follow RELEASING.md to tag.")
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("run this inside the gofastr-native repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func run(c *checker, root, version string, skipTests bool) {
	if !semver.IsValid(version) || semver.Canonical(version) != version {
		c.record(fail, "version", fmt.Sprintf("%q is not a canonical semantic version such as v0.1.0", version))
		return
	}
	c.record(pass, "version", version)

	checkChangelog(c, root, version)
	checkWorkingTree(c, root)
	checkTags(c, root, version)
	checkHygiene(c, root)

	clone, cleanup, err := freshClone(root)
	if err != nil {
		c.record(fail, "fresh clone", err.Error())
		return
	}
	defer cleanup()
	c.record(pass, "fresh clone", "HEAD cloned to a temporary directory; checks below use it with GOWORK=off")

	modules := moduleDirs(clone)
	for _, dir := range modules {
		checkModule(c, clone, dir, skipTests)
	}
	checkNode(c, clone, skipTests)
	checkLicenses(c, clone, modules)
	printNextSteps(version)
}

var changelogHeading = regexp.MustCompile(`(?m)^## (v\S+) \((\d{4}-\d{2}-\d{2})\)\s*$`)

func checkChangelog(c *checker, root, version string) {
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		c.record(fail, "CHANGELOG.md", err.Error())
		return
	}
	for _, m := range changelogHeading.FindAllStringSubmatch(string(data), -1) {
		if m[1] == version {
			if _, err := time.Parse(time.DateOnly, m[2]); err != nil {
				c.record(fail, "CHANGELOG.md", "the "+version+" heading has an invalid date")
				return
			}
			c.record(pass, "CHANGELOG.md", "has ## "+version+" ("+m[2]+")")
			return
		}
	}
	c.record(fail, "CHANGELOG.md", "no heading '## "+version+" (YYYY-MM-DD)'; write the entry before tagging")
}

func checkWorkingTree(c *checker, root string) {
	out, err := command(root, nil, "git", "status", "--porcelain")
	switch {
	case err != nil:
		c.record(fail, "working tree", err.Error())
	case strings.TrimSpace(out) != "":
		c.record(fail, "working tree", "uncommitted changes; the tag would not include them:\n"+out)
	default:
		c.record(pass, "working tree", "clean")
	}
}

func tagNames(version string) []string {
	var tags []string
	for _, dir := range repocheck.ReleasedModules {
		if dir == "." {
			tags = append(tags, version)
		} else {
			tags = append(tags, dir+"/"+version)
		}
	}
	return tags
}

func checkTags(c *checker, root, version string) {
	tags := tagNames(version)
	for _, tag := range tags {
		if out, _ := command(root, nil, "git", "tag", "--list", tag); strings.TrimSpace(out) != "" {
			c.record(fail, "tag "+tag, "already exists locally")
			continue
		}
		out, err := command(root, nil, "git", "ls-remote", "--tags", "origin", "refs/tags/"+tag)
		switch {
		case err != nil:
			c.record(notRun, "tag "+tag, "could not reach origin: "+firstLine(err.Error()))
		case strings.TrimSpace(out) != "":
			c.record(fail, "tag "+tag, "already exists on origin")
		default:
			c.record(pass, "tag "+tag, "unused locally and on origin")
		}
	}
}

func checkHygiene(c *checker, root string) {
	findings, err := repocheck.Check(root)
	if err != nil {
		c.record(fail, "repository hygiene", err.Error())
		return
	}
	if len(findings) == 0 {
		c.record(pass, "repository hygiene", "no machine paths, tool names, credentials, stale claims or uncredited notices")
		return
	}
	var b strings.Builder
	for _, f := range findings {
		b.WriteString("\n  " + f.String())
	}
	c.record(fail, "repository hygiene", fmt.Sprintf("%d findings%s", len(findings), b.String()))
}

func freshClone(root string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "gofastr-native-releasecheck-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	clone := filepath.Join(dir, "repo")
	if _, err := command(root, nil, "git", "clone", "--quiet", "--no-hardlinks", root, clone); err != nil {
		cleanup()
		return "", nil, err
	}
	return clone, cleanup, nil
}

// moduleDirs lists every module in the clone, released modules first.
func moduleDirs(clone string) []string {
	out, err := command(clone, nil, "git", "ls-files", "--", "go.mod", "*/go.mod")
	if err != nil {
		return slices.Clone(repocheck.ReleasedModules)
	}
	var dirs []string
	for line := range strings.Lines(out) {
		if line = strings.TrimSpace(line); line != "" {
			dirs = append(dirs, path.Dir(line))
		}
	}
	slices.SortFunc(dirs, func(a, b string) int {
		ra, rb := slices.Contains(repocheck.ReleasedModules, a), slices.Contains(repocheck.ReleasedModules, b)
		if ra != rb {
			if ra {
				return -1
			}
			return 1
		}
		return strings.Compare(a, b)
	})
	return dirs
}

func goEnv() []string {
	return []string{"GOWORK=off", "GOFLAGS=-mod=readonly"}
}

func checkModule(c *checker, clone, dir string, skipTests bool) {
	moddir := filepath.Join(clone, filepath.FromSlash(dir))
	name := "module " + dir
	if _, err := command(moddir, goEnv(), "go", "mod", "tidy", "-diff"); err != nil {
		c.record(fail, name+" tidy", "go mod tidy would change go.mod or go.sum: "+firstLine(err.Error()))
	} else {
		c.record(pass, name+" tidy", "")
	}
	if out, err := command(moddir, nil, gofmtPath(), "-l", "."); err != nil || strings.TrimSpace(out) != "" {
		c.record(fail, name+" gofmt", strings.TrimSpace(out+" "+errString(err)))
	} else {
		c.record(pass, name+" gofmt", "")
	}
	if _, err := command(moddir, goEnv(), "go", "vet", "./..."); err != nil {
		c.record(fail, name+" vet", err.Error())
	} else {
		c.record(pass, name+" vet", "")
	}
	if skipTests {
		c.record(notRun, name+" test", "-skip-tests")
		return
	}
	if _, err := command(moddir, goEnv(), "go", "test", "-count=1", "./..."); err != nil {
		c.record(fail, name+" test", err.Error())
	} else {
		c.record(pass, name+" test", "")
	}
}

func gofmtPath() string {
	if out, err := exec.Command("go", "env", "GOROOT").Output(); err == nil {
		p := filepath.Join(strings.TrimSpace(string(out)), "bin", "gofmt")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		if _, err := os.Stat(p + ".exe"); err == nil {
			return p + ".exe"
		}
	}
	return "gofmt"
}

func checkNode(c *checker, clone string, skipTests bool) {
	files, _ := filepath.Glob(filepath.Join(clone, "render", "webview", "*.test.cjs"))
	switch {
	case len(files) == 0:
		return
	case skipTests:
		c.record(notRun, "WebView node tests", "-skip-tests")
		return
	}
	if _, err := exec.LookPath("node"); err != nil {
		c.record(notRun, "WebView node tests", "node is not on PATH")
		return
	}
	if _, err := command(clone, nil, "node", append([]string{"--test"}, files...)...); err != nil {
		c.record(fail, "WebView node tests", err.Error())
	} else {
		c.record(pass, "WebView node tests", fmt.Sprintf("%d files", len(files)))
	}
}

type dep struct{ path, version, dir string }

// checkLicenses classifies the license of every non-standard-library module
// that a module's packages, tests or go tool directives build.
func checkLicenses(c *checker, clone string, modules []string) {
	deps := map[string]dep{}
	for _, dir := range modules {
		moddir := filepath.Join(clone, filepath.FromSlash(dir))
		patterns := [][]string{{"./..."}}
		if data, err := os.ReadFile(filepath.Join(moddir, "go.mod")); err == nil && bytes.Contains(data, []byte("\ntool")) {
			patterns = append(patterns, []string{"tool"})
		}
		for _, p := range patterns {
			args := append([]string{"list", "-deps", "-test", "-f", "{{with .Module}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}"}, p...)
			out, err := command(moddir, goEnv(), "go", args...)
			if err != nil {
				c.record(fail, "licenses", "go list in "+dir+": "+firstLine(err.Error()))
				return
			}
			for line := range strings.Lines(out) {
				parts := strings.Split(strings.TrimSpace(line), "\t")
				if len(parts) != 3 || parts[0] == repocheck.ModulePath || strings.HasPrefix(parts[0], repocheck.ModulePath+"/") {
					continue
				}
				deps[parts[0]] = dep{parts[0], parts[1], parts[2]}
			}
		}
	}
	byLicense := map[string][]string{}
	var bad []string
	for _, d := range deps {
		file, license := repocheck.ModuleLicense(d.dir)
		label := d.path + "@" + d.version
		byLicense[license] = append(byLicense[license], label)
		if file == "" {
			bad = append(bad, label+": no LICENSE file")
		} else if !slices.Contains(repocheck.AllowedLicenses, license) {
			bad = append(bad, label+": "+license+" ("+file+")")
		}
	}
	var summary []string
	for license, mods := range byLicense {
		summary = append(summary, fmt.Sprintf("%s %d", license, len(mods)))
	}
	slices.Sort(summary)
	if len(bad) > 0 {
		slices.Sort(bad)
		c.record(fail, "licenses", "review before release (allowed: "+strings.Join(repocheck.AllowedLicenses, ", ")+"):\n  "+strings.Join(bad, "\n  "))
		return
	}
	c.record(pass, "licenses", fmt.Sprintf("%d dependency modules: %s", len(deps), strings.Join(summary, ", ")))
}

func printNextSteps(version string) {
	tags := tagNames(version)
	fmt.Println()
	fmt.Println("When every check passes and the release commit is on main:")
	fmt.Println()
	for _, tag := range tags {
		fmt.Printf("  git tag -a %s -m %q <main commit>\n", tag, tag)
	}
	fmt.Printf("  git push origin %s\n", strings.Join(tags, " "))
	fmt.Printf("  gh release create %s --title %s --notes-file <the %s section of CHANGELOG.md>\n", version, version, version)
}

// command runs name in dir with extra environment and returns its stdout.
// On failure the error carries the command and the end of its output.
func command(dir string, env []string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("%s %s: %v\n%s", name, strings.Join(args, " "), err, tail(stdout.String()+stderr.String(), 40))
	}
	return stdout.String(), nil
}

func tail(s string, lines int) string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(all) > lines {
		all = append([]string{"..."}, all[len(all)-lines:]...)
	}
	return strings.Join(all, "\n")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
