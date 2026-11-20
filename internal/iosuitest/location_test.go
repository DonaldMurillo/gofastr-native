// Package iosuitest exercises installed native apps through simulator UI tests.
package iosuitest

import (
	"context"
	"embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/*
var sources embed.FS

func TestLocationSimulatorUI(t *testing.T) {
	if os.Getenv("GOFASTR_IOS_UI") != "1" {
		t.Skip("set GOFASTR_IOS_UI=1 to build and run simulator UI acceptance")
	}
	if runtime.GOOS != "darwin" {
		t.Fatal("iOS UI acceptance requires macOS and Xcode")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	artifacts := os.Getenv("GOFASTR_IOS_UI_ARTIFACTS")
	if artifacts == "" {
		artifacts = filepath.Join(home, "artifacts", "location-ui")
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(artifacts, "run-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("UI artifacts: %s", work)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	run := func(name string, args ...string) string {
		t.Helper()
		command := exec.CommandContext(ctx, name, args...)
		command.Dir = root
		command.Env = append(os.Environ(), "GOWORK=off")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, output)
		}
		return strings.TrimSpace(string(output))
	}
	var available struct {
		Runtimes []struct {
			Identifier  string
			IsAvailable bool
		}
	}
	if err := json.Unmarshal([]byte(run("xcrun", "simctl", "list", "runtimes", "--json")), &available); err != nil {
		t.Fatal(err)
	}
	var simulatorRuntime string
	for _, candidate := range available.Runtimes {
		if candidate.IsAvailable && strings.Contains(candidate.Identifier, ".iOS-") {
			simulatorRuntime = candidate.Identifier
			break
		}
	}
	if simulatorRuntime == "" {
		t.Fatal("no available iOS simulator runtime")
	}
	udid := run("xcrun", "simctl", "create", "GoFastr Location UI", "com.apple.CoreSimulator.SimDeviceType.iPhone-17", simulatorRuntime)
	t.Cleanup(func() {
		for _, action := range []string{"shutdown", "delete"} {
			if output, err := exec.Command("xcrun", "simctl", action, udid).CombinedOutput(); err != nil {
				t.Logf("simulator cleanup: %v: %s", err, output)
			}
		}
	})
	run("xcrun", "simctl", "boot", udid)
	run("xcrun", "simctl", "bootstatus", udid, "-b")
	out := filepath.Join(work, "demo")
	build := run("go", "run", "./cmd/gofastr-native", "build", "--platform", "ios-sim", "--renderer", "native", "--pkg", "./examples/devicefeatures/featuresnative", "--id", "dev.gofastr.locationdemo", "--name", "Location Demo", "--location", "--location-usage", "Show your position in the foreground location demo.", "--out", out)
	if err := os.WriteFile(filepath.Join(work, "build.log"), []byte(build), 0o600); err != nil {
		t.Fatal(err)
	}
	apps, err := filepath.Glob(filepath.Join(out, "*.app"))
	if err != nil || len(apps) != 1 {
		t.Fatalf("want one built app, got %v: %v", apps, err)
	}
	run("xcrun", "simctl", "install", udid, apps[0])
	run("xcrun", "simctl", "privacy", udid, "grant", "location", "dev.gofastr.locationdemo")
	run("xcrun", "simctl", "location", udid, "set", "37.3349,-122.0090")
	run("xcrun", "simctl", "launch", udid, "dev.gofastr.locationdemo")
	project := filepath.Join(work, "LocationUI.xcodeproj")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"project.pbxproj", "LocationUI.swift"} {
		content, err := sources.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(work, name)
		if name == "project.pbxproj" {
			path = filepath.Join(project, name)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	results := filepath.Join(work, "LocationUI.xcresult")
	output := run("xcodebuild", "-project", project, "-scheme", "LocationUI", "-destination", "platform=iOS Simulator,id="+udid, "-derivedDataPath", filepath.Join(work, "derived"), "-resultBundlePath", results, "-parallel-testing-enabled", "NO", "test", "CODE_SIGNING_ALLOWED=NO")
	if err := os.WriteFile(filepath.Join(work, "ui-test.log"), []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "passed") || strings.Contains(line, "Executed") || strings.Contains(line, "TEST SUCCEEDED") {
			t.Log(line)
		}
	}
	run("xcrun", "xcresulttool", "export", "attachments", "--path", results, "--output-path", filepath.Join(work, "screenshots"))
	t.Logf("Coordinate screenshot attachments: %s", filepath.Join(work, "screenshots"))
}
