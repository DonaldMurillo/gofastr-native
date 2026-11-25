package arkit_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGalleryARUI(t *testing.T) {
	if os.Getenv("GOFASTR_IOS_UITEST") != "1" {
		t.Skip("set GOFASTR_IOS_UITEST=1 for simulator UI acceptance")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS")
	}
	if _, err := exec.LookPath("xcodebuild"); err != nil {
		t.Skip("requires Xcode")
	}
	device := os.Getenv("GOFASTR_IOS_DEVICE")
	if device == "" {
		t.Fatal("GOFASTR_IOS_DEVICE must name a booted simulator UDID")
	}
	output := os.Getenv("GOFASTR_IOS_UITEST_OUT")
	if output == "" {
		t.Fatal("GOFASTR_IOS_UITEST_OUT must name an output directory outside the repository")
	}
	output, err := filepath.Abs(output)
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if relative, err := filepath.Rel(root, output); err != nil || !(relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatal("output must be outside the repository")
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off")
		data, err := cmd.CombinedOutput()
		t.Logf("%s: %s", name, data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	run("go", "run", "./cmd/gofastr-native", "build", "--platform", "ios-sim", "--renderer", "native", "--pkg", "./examples/gallery/gallerynative", "--out", output)
	run("xcrun", "simctl", "install", device, filepath.Join(output, "Gallerynative.app"))
	run("xcodebuild", "test", "-project", "extension/arkit/uitest/ARKitUITests.xcodeproj", "-scheme", "ARKitUITests", "-destination", "platform=iOS Simulator,id="+device, "-parallel-testing-enabled", "NO", "-collect-test-diagnostics", "never", "-derivedDataPath", filepath.Join(output, "DerivedData"), "-resultBundlePath", filepath.Join(output, "ARKit.xcresult"), "CODE_SIGNING_ALLOWED=NO")
	run("xcrun", "simctl", "io", device, "screenshot", filepath.Join(output, "ar-screen.png"))
}
