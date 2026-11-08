// Command release checks and publishes a gofastr-native version. See
// RELEASING.md.
//
//	go run ./internal/release check vX.Y.Z               # on the release branch, before the PR merges
//	go run ./internal/release check -skip-tests vX.Y.Z   # the same without go test and node --test
//	go run ./internal/release publish -dry-run vX.Y.Z    # on main after the merge: show what publish would do
//	go run ./internal/release publish vX.Y.Z             # tag, push the tags, create the GitHub release
//
// check changes nothing. It verifies the version and CHANGELOG entry, a
// clean working tree, unused tags, the repocheck hygiene rules, then a fresh
// clone of HEAD with GOWORK=off (tidy, gofmt, vet, tests, the WebView Node
// tests) and the license of every dependency module.
//
// publish requires HEAD to be origin/main with a passing CI run, repeats the
// checks without tests, asks you to type the version, then creates an
// annotated tag per released module, pushes them in one atomic push, creates
// the GitHub release from the CHANGELOG section, and checks that
// proxy.golang.org serves every released module at the new version.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr-native/internal/repocheck"
)

type status string

const (
	pass   status = "PASS"
	fail   status = "FAIL"
	notRun status = "NOT RUN"
)

type checker struct {
	out    io.Writer
	failed bool
}

func (c *checker) record(s status, name, detail string) {
	line := fmt.Sprintf("%-7s %s", s, name)
	if detail != "" {
		line += ": " + detail
	}
	fmt.Fprintln(c.out, line)
	if s == fail {
		c.failed = true
	}
}

const usage = `usage:
  go run ./internal/release check [-skip-tests] vX.Y.Z
  go run ./internal/release publish [-dry-run] [-yes] vX.Y.Z
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	c := &checker{out: os.Stdout}
	switch sub, args := os.Args[1], os.Args[2:]; sub {
	case "check":
		fs := flag.NewFlagSet("check", flag.ExitOnError)
		skipTests := fs.Bool("skip-tests", false, "skip go test and node --test (tidy, gofmt, vet and licenses still run)")
		version := parseVersion(fs, args)
		reason := ""
		if *skipTests {
			reason = "-skip-tests"
		}
		runChecks(c, root, version, reason)
		fmt.Println()
		if c.failed {
			fmt.Println("Not ready: fix every FAIL above, commit, and run this again.")
			os.Exit(1)
		}
		fmt.Println("Ready. Review any NOT RUN lines. After the PR merges and CI passes on main:")
		fmt.Println()
		fmt.Println("  git switch main && git pull --ff-only")
		fmt.Printf("  go run ./internal/release publish %s\n", version)
	case "publish":
		fs := flag.NewFlagSet("publish", flag.ExitOnError)
		dryRun := fs.Bool("dry-run", false, "run every check and print the commands without tagging, pushing or releasing")
		yes := fs.Bool("yes", false, "do not ask for confirmation")
		version := parseVersion(fs, args)
		ok := publish(c, root, version, publishOptions{dryRun: *dryRun, yes: *yes, in: os.Stdin})
		if !ok {
			os.Exit(1)
		}
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func parseVersion(fs *flag.FlagSet, args []string) string {
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage); fs.PrintDefaults() }
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	return fs.Arg(0)
}

func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("run this inside the gofastr-native repository: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// tagNames lists the tags a version needs: vX.Y.Z for the root module and
// <dir>/vX.Y.Z for each other released module.
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

// modulePaths lists the import paths of the released modules.
func modulePaths() []string {
	var paths []string
	for _, dir := range repocheck.ReleasedModules {
		if dir == "." {
			paths = append(paths, repocheck.ModulePath)
		} else {
			paths = append(paths, repocheck.ModulePath+"/"+dir)
		}
	}
	return paths
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
