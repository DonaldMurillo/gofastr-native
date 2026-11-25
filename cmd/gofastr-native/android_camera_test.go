package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	renderandroid "github.com/DonaldMurillo/gofastr-native/render/android"
)

func TestAndroidIntentCameraPermission(t *testing.T) {
	source, err := renderandroid.Sources.ReadFile("Services.java")
	if err != nil {
		t.Fatal(err)
	}
	capture := androidJavaSection(t, string(source), "    public void capture(", "    private static boolean calendarText(")
	permissions := androidJavaSection(t, string(source), "    void permissionsResult(", "\n}")
	runAndroidJavaFixture(t, "CameraPermission", `import java.io.File;
import java.util.Arrays;
import java.util.function.Consumer;

class CameraPermission {
    static class Manifest { static class permission { static final String CAMERA = "android.permission.CAMERA"; } }
    static class PackageInfo { String[] requestedPermissions; }
    static class PackageManager {
        static final int GET_PERMISSIONS=1, PERMISSION_GRANTED=0;
        static class NameNotFoundException extends Exception {}
        PackageInfo info = new PackageInfo();
        PackageInfo getPackageInfo(String name,int flags) { return info; }
    }
    static class Uri {}
    static class ClipData { static ClipData newRawUri(String label,Uri uri) { return new ClipData(); } }
    static class Intent {
        static final int FLAG_GRANT_READ_URI_PERMISSION=1, FLAG_GRANT_WRITE_URI_PERMISSION=2;
        Intent(String action) {}
        Intent putExtra(String key,Uri uri) { return this; }
        Intent addFlags(int flags) { return this; }
        void setClipData(ClipData clip) {}
    }
    static class MediaStore { static final String ACTION_IMAGE_CAPTURE="capture", EXTRA_OUTPUT="output"; }
    static class TransferProvider {
        static File create(Host host,String name) { return new File(name); }
        static Uri uri(Host host,File file) { return new Uri(); }
    }
    static class DeviceLocation {
        static final int PERMISSION_REQUEST=497;
        void permissionsResult(int[] grants) { throw new AssertionError("wrong callback"); }
    }
    static class JSON { static String object(String key,String value) { return value; } }
    static class Host {
        PackageManager manager=new PackageManager();
        boolean granted;
        int requests, launches;
        String getPackageName() { return "test"; }
        PackageManager getPackageManager() { return manager; }
        boolean canUseCamera() { return true; }
        int checkSelfPermission(String permission) { return granted ? 0 : -1; }
        void requestPermissions(String[] names,int code) {
            if (code!=CAMERA || !Arrays.equals(names,new String[]{Manifest.permission.CAMERA})) throw new AssertionError();
            requests++;
        }
        void startActivityForResult(Intent intent,int code) {
            if (manager.info.requestedPermissions!=null && !granted) throw new SecurityException("declared but not granted");
            launches++;
        }
    }
    static final int CAMERA=403, MICROPHONE=405, NOTIFICATION=406;
    final DeviceLocation location=new DeviceLocation();
    final Host host=new Host();
    Runnable permissionGranted;
    Consumer<Uri> picked;
    File cameraFile;
    Object result=new Object();
    String status;
    int activityCode;
    String outcome(String status,String message) { return status; }
    void finish(String value) { status=value; }
    void failed(Exception error) { throw new AssertionError(error); }
`+capture+permissions+`
    public static void main(String[] args) {
        for (String mode : new String[]{"undeclared","granted","allow","deny"}) {
            CameraPermission service=new CameraPermission();
            Host host=service.host;
            if (!"undeclared".equals(mode)) host.manager.info.requestedPermissions=new String[]{Manifest.permission.CAMERA};
            host.granted="granted".equals(mode);
            Consumer<Uri> callback=uri->{};
            service.capture(callback);
            boolean prompts="allow".equals(mode) || "deny".equals(mode);
            if (host.requests!=(prompts ? 1 : 0) || host.launches!=(prompts ? 0 : 1)) throw new AssertionError(mode);
            if ("allow".equals(mode)) { host.granted=true; service.permissionsResult(CAMERA,new int[]{0}); }
            if ("deny".equals(mode)) { service.permissionsResult(CAMERA,new int[]{-1}); }
            if ("deny".equals(mode)) {
                if (!"denied".equals(service.status) || host.launches!=0) throw new AssertionError(mode);
            } else if (host.launches!=1 || service.picked!=callback || service.activityCode!=CAMERA) throw new AssertionError(mode);
        }
    }
}`)
}

func androidJavaSection(t *testing.T, source, start, end string) string {
	t.Helper()
	_, rest, foundStart := strings.Cut(source, start)
	body, _, foundEnd := strings.Cut(rest, end)
	if !foundStart || !foundEnd {
		t.Fatalf("Java section not found: %s ... %s", start, end)
	}
	return start + body
}

func runAndroidJavaFixture(t *testing.T, class, source string) {
	t.Helper()
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("Java runtime unavailable")
	}
	path := filepath.Join(t.TempDir(), class+".java")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(java, path).CombinedOutput(); err != nil {
		t.Fatalf("Java fixture %s: %v\n%s", class, err, output)
	}
}
