package ios

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// requireSwift skips executable Swift checks unless the macOS toolchain is present.
func requireSwift(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("executable Swift Foundation checks require macOS")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("the Swift toolchain is not installed")
	}
}

// rendererSourceFiles lists every renderer Swift source compiled by the executable Foundation checks.
var rendererSourceFiles = []string{
	"GofastrComponents.swift", "GofastrComponentExtensions.swift", "GofastrActions.swift", "GofastrFiles.swift",
	"GofastrPresentation.swift", "GofastrImages.swift", "GofastrAttachments.swift", "GofastrServices.swift",
	"GofastrIncomingShares.swift", "GofastrSecureStorage.swift", "GofastrCalendar.swift", "GofastrWidgets.swift",
}

// readSource returns one source file of fsys as a string.
func readSource(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	source, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	return string(source)
}

// swiftSources materializes the named sources into directory and returns their paths.
func swiftSources(t *testing.T, directory string, fsys fs.FS, names ...string) []string {
	t.Helper()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte(readSource(t, fsys, name)), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

// swiftDirectorySources materializes every .swift file of sources into directory.
func swiftDirectorySources(t *testing.T, directory string, sources fs.FS) []string {
	t.Helper()
	entries, err := fs.ReadDir(sources, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".swift") {
			names = append(names, entry.Name())
		}
	}
	return swiftSources(t, directory, sources, names...)
}

// draftDTOs returns the binding-independent DTO slice of GofastrNative.swift.
func draftDTOs(t *testing.T) string {
	t.Helper()
	native := readSource(t, Sources, "GofastrNative.swift")
	start := strings.Index(native, "struct MobileDraft:")
	end := strings.Index(native, "final class MobileHost {")
	if start < 0 || end <= start {
		t.Fatal("missing actual renderer DTO boundary")
	}
	return native[start:end]
}

// compileRunChecks compiles checks (after the pre source paths and swiftc flags)
// into the executable named name inside directory, then runs it with post
// arguments. compileErr and runErr keep each check's failure wording; an empty
// runErr skips execution and only the executable path is returned.
func compileRunChecks(t *testing.T, directory, name, checks, compileErr, runErr string, pre, post []string) string {
	t.Helper()
	checksPath := filepath.Join(directory, name+".swift")
	if err := os.WriteFile(checksPath, []byte(checks), 0o600); err != nil {
		t.Fatal(err)
	}
	arguments := append([]string{"--sdk", "macosx", "swiftc", "-parse-as-library", "-module-cache-path", filepath.Join(directory, "module-cache")}, pre...)
	executable := filepath.Join(directory, name)
	arguments = append(arguments, checksPath, "-o", executable)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, "xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", compileErr, err, output)
	}
	if runErr != "" {
		if output, err := exec.CommandContext(ctx, executable, post...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", runErr, err, output)
		}
	}
	return executable
}

// simulatorArchitecture maps the host architecture onto the Swift target triple.
func simulatorArchitecture() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}

// uikitSimulatorArguments builds the shared iOS simulator swiftc argument
// header around the per-check flags; it skips unless the simulator SDK exists.
func uikitSimulatorArguments(t *testing.T, directory string, flags ...string) []string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("UIKit compilation requires Xcode on macOS")
	}
	sdk, err := exec.Command("xcrun", "--sdk", "iphonesimulator", "--show-sdk-path").Output()
	if err != nil {
		t.Skipf("no iOS simulator SDK: %v", err)
	}
	return append([]string{"--sdk", "iphonesimulator", "swiftc"},
		append(flags,
			"-target", simulatorArchitecture()+"-apple-ios16.0-simulator",
			"-sdk", strings.TrimSpace(string(sdk)),
			"-module-cache-path", filepath.Join(directory, "module-cache"))...)
}

// requireXcrun runs xcrun with arguments and fails with fail as the message.
func requireXcrun(t *testing.T, fail string, arguments ...string) {
	t.Helper()
	if output, err := exec.Command("xcrun", arguments...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", fail, err, output)
	}
}

// requireTokens fails when source no longer contains any token.
func requireTokens(t *testing.T, source, message string, tokens ...string) {
	t.Helper()
	for _, token := range tokens {
		if !strings.Contains(source, token) {
			t.Errorf("%s: %s", message, token)
		}
	}
}

// requireTokenMap applies requireTokens to every checked source file; format
// receives the file name as its single argument.
func requireTokenMap(t *testing.T, checks map[string][]string, format string) {
	t.Helper()
	for name, tokens := range checks {
		requireTokens(t, readSource(t, Sources, name), fmt.Sprintf(format, name), tokens...)
	}
}
