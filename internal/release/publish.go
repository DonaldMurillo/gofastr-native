package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

type publishOptions struct {
	dryRun bool
	yes    bool
	in     io.Reader
}

// publish tags and releases version. It reports whether every step passed.
// Nothing changes until every check passes and the version is confirmed.
func publish(c *checker, root, version string, opts publishOptions) bool {
	sha, ok := checkOnMain(c, root)
	if !ok {
		return false
	}
	if _, err := exec.LookPath("gh"); err != nil {
		c.record(fail, "gh", "the GitHub CLI is needed to check CI and create the release")
		return false
	}
	checkCI(c, root, sha)
	runChecks(c, root, version, "CI on main ran the tests on this commit")
	if c.failed {
		fmt.Fprintln(c.out, "\nNothing was tagged or pushed. Fix every FAIL above and run this again.")
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		c.record(fail, "release notes", err.Error())
		return false
	}
	notes := changelogSection(string(data), version)
	if notes == "" {
		c.record(fail, "release notes", "the "+version+" section of CHANGELOG.md is empty")
		return false
	}

	tags := tagNames(version)
	fmt.Fprintf(c.out, "\nPublish %s from %s (origin/main):\n\n", version, sha[:12])
	for _, tag := range tags {
		fmt.Fprintf(c.out, "  git tag -a %s -m %s %s\n", tag, tag, sha[:12])
	}
	fmt.Fprintf(c.out, "  git push --atomic origin %s\n", strings.Join(tags, " "))
	fmt.Fprintf(c.out, "  gh %s\n\n", strings.Join(ghReleaseArgs(version, "<the CHANGELOG section>"), " "))
	if opts.dryRun {
		c.record(notRun, "publish", "-dry-run; nothing was tagged, pushed or released")
		return true
	}
	if !opts.yes && !confirm(c.out, opts.in, version) {
		c.record(notRun, "publish", "not confirmed; nothing was tagged, pushed or released")
		return false
	}

	var created []string
	undo := func() {
		for _, tag := range created {
			_, _ = command(root, nil, "git", "tag", "-d", tag)
		}
	}
	for _, tag := range tags {
		if _, err := command(root, nil, "git", "tag", "-a", tag, "-m", tag, sha); err != nil {
			undo()
			c.record(fail, "tag "+tag, err.Error())
			return false
		}
		created = append(created, tag)
	}
	c.record(pass, "tags", strings.Join(tags, ", ")+" at "+sha[:12])
	if _, err := command(root, nil, "git", append([]string{"push", "--atomic", "origin"}, tags...)...); err != nil {
		undo()
		c.record(fail, "push tags", err.Error()+"\nthe local tags were deleted; nothing reached origin")
		return false
	}
	c.record(pass, "push tags", "origin")

	notesFile, err := writeTemp(notes)
	if err != nil {
		c.record(fail, "GitHub release", err.Error())
		return false
	}
	defer os.Remove(notesFile)
	if out, err := command(root, nil, "gh", ghReleaseArgs(version, notesFile)...); err != nil {
		c.record(fail, "GitHub release", err.Error()+"\nthe tags are pushed; create the release by hand from the CHANGELOG section")
	} else {
		c.record(pass, "GitHub release", strings.TrimSpace(out))
	}
	checkProxy(c, version)
	return !c.failed
}

// ghReleaseArgs is the gh command line that creates the GitHub release.
// --verify-tag makes gh refuse to create a tag of its own.
func ghReleaseArgs(version, notesFile string) []string {
	args := []string{"release", "create", version, "--title", version, "--verify-tag", "--notes-file", notesFile}
	if semver.Prerelease(version) != "" {
		args = append(args, "--prerelease")
	}
	return args
}

// checkOnMain requires a clean HEAD equal to origin/main after a fetch, and
// returns its commit.
func checkOnMain(c *checker, root string) (string, bool) {
	if _, err := command(root, nil, "git", "fetch", "--quiet", "--tags", "origin", "main"); err != nil {
		c.record(fail, "fetch origin", err.Error())
		return "", false
	}
	head, err1 := command(root, nil, "git", "rev-parse", "HEAD")
	main, err2 := command(root, nil, "git", "rev-parse", "origin/main")
	if err := errors.Join(err1, err2); err != nil {
		c.record(fail, "on main", err.Error())
		return "", false
	}
	head, main = strings.TrimSpace(head), strings.TrimSpace(main)
	if head != main {
		c.record(fail, "on main", fmt.Sprintf("HEAD is %s but origin/main is %s; publish tags the merged commit: git switch main && git pull --ff-only", head[:12], main[:12]))
		return "", false
	}
	c.record(pass, "on main", head[:12]+" is origin/main")
	return head, true
}

type ghRun struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url"`
}

// checkCI requires the CI workflow's push run for sha to have succeeded.
func checkCI(c *checker, root, sha string) {
	out, err := command(root, nil, "gh", "run", "list", "--workflow", "ci.yml", "--commit", sha, "--event", "push", "--limit", "1", "--json", "status,conclusion,url")
	if err != nil {
		c.record(fail, "CI on main", firstLine(err.Error()))
		return
	}
	var runs []ghRun
	if err := json.Unmarshal([]byte(out), &runs); err != nil {
		c.record(fail, "CI on main", "cannot read gh output: "+err.Error())
		return
	}
	switch {
	case len(runs) == 0:
		c.record(fail, "CI on main", "no CI run for "+sha[:12]+" yet; wait for the push to main to start one")
	case runs[0].Status != "completed":
		c.record(fail, "CI on main", "still "+runs[0].Status+": "+runs[0].URL)
	case runs[0].Conclusion != "success":
		c.record(fail, "CI on main", runs[0].Conclusion+": "+runs[0].URL)
	default:
		c.record(pass, "CI on main", runs[0].URL)
	}
}

func confirm(out io.Writer, in io.Reader, version string) bool {
	fmt.Fprintf(out, "Type %s to publish it: ", version)
	line, _ := bufio.NewReader(in).ReadString('\n')
	return strings.TrimSpace(line) == version
}

// changelogSection returns the body of CHANGELOG.md's "## version (date)"
// section, without its heading, up to the next "## " heading.
func changelogSection(changelog, version string) string {
	for _, loc := range changelogHeading.FindAllStringSubmatchIndex(changelog, -1) {
		if changelog[loc[2]:loc[3]] != version {
			continue
		}
		body := changelog[loc[1]:]
		if next := strings.Index(body, "\n## "); next >= 0 {
			body = body[:next]
		}
		return strings.TrimSpace(body) + "\n"
	}
	return ""
}

func writeTemp(content string) (string, error) {
	f, err := os.CreateTemp("", "gofastr-native-release-notes-*.md")
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}

// checkProxy asks proxy.golang.org for each released module at version. The
// first request makes the proxy fetch the tag, so it retries for a while.
func checkProxy(c *checker, version string) {
	dir, err := os.MkdirTemp("", "gofastr-native-proxy-")
	if err != nil {
		c.record(fail, "module proxy", err.Error())
		return
	}
	defer os.RemoveAll(dir)
	env := []string{"GOWORK=off", "GOFLAGS=-mod=mod", "GOPROXY=https://proxy.golang.org", "GONOSUMDB=", "GOPRIVATE=", "GONOPROXY="}
	for _, mod := range modulePaths() {
		var err error
		for attempt := 1; attempt <= 6; attempt++ {
			if _, err = command(dir, env, "go", "list", "-m", mod+"@"+version); err == nil || attempt == 6 {
				break
			}
			time.Sleep(time.Duration(attempt) * 10 * time.Second) // 2.5 minutes in all
		}
		if err != nil {
			c.record(fail, "module proxy "+mod, firstLine(err.Error())+"; retry later with: GOPROXY=https://proxy.golang.org go list -m "+mod+"@"+version)
		} else {
			c.record(pass, "module proxy "+mod, version+" resolves")
		}
	}
}
