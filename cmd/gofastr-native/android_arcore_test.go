package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	renderarcore "github.com/DonaldMurillo/gofastr-native/render/android/arcore"
)

func TestARCoreIsOptIn(t *testing.T) {
	a := nativeApp(t).forAndroid(34)
	plain, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	a.ARCoreAAR = "core.aar"
	enabled, err := render("templates/android/AndroidManifest.xml.tmpl", a)
	if err != nil {
		t.Fatal(err)
	}
	wellFormed(t, "AR Optional manifest", enabled)
	for _, marker := range []string{"android.permission.CAMERA", "com.google.ar.core", "ARSessionActivity"} {
		if bytes.Contains(plain, []byte(marker)) || !bytes.Contains(enabled, []byte(marker)) {
			t.Errorf("%q should only occur in the explicitly enabled manifest", marker)
		}
	}
	if !bytes.Contains(enabled, []byte(`android:name="com.google.ar.core" android:value="optional"`)) {
		t.Fatal("AR must not exclude non-AR hardware")
	}
	if err := validateDeviceFeatures(a, "android-emu"); err != nil {
		t.Fatal(err)
	}
	if validateDeviceFeatures(a, "ios-sim") == nil {
		t.Fatal("ARCore works only on Android")
	}
	a.Renderer = "webview"
	if validateDeviceFeatures(a, "android-emu") == nil {
		t.Fatal("ARCore works only in the native renderer")
	}
}

func TestARCoreClosesSessionAfterPauseOffMainThread(t *testing.T) {
	source, err := renderarcore.Sources.ReadFile("ARSessionActivity.java")
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := androidJavaSection(t, string(source), "    @Override protected void onPause()", "    private void place(")
	runAndroidJavaFixture(t, "ARSessionLifecycle", `import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;

class ARSessionLifecycle extends Activity {
    final Surface surface = new Surface();
    Session session = new Session();
    boolean running = true;

    class Surface {
        boolean paused;
        void onPause() { paused = true; }
    }

    class Session {
        final Thread main = Thread.currentThread();
        final CountDownLatch closed = new CountDownLatch(1);
        boolean paused;
        void pause() {
            if (Thread.currentThread() != main || !surface.paused) throw new AssertionError("pause before renderer stops");
            paused = true;
        }
        void close() {
            if (Thread.currentThread() == main || !paused || session != null) throw new AssertionError("unsafe session close");
            closed.countDown();
        }
    }

`+lifecycle+`
    public static void main(String[] args) throws Exception {
        ARSessionLifecycle activity = new ARSessionLifecycle();
        Session session = activity.session;
        activity.onPause();
        activity.onDestroy();
        if (activity.session != null || !session.closed.await(2, TimeUnit.SECONDS)) throw new AssertionError("session not closed");
    }
}

class Activity {
    protected void onPause() {}
    protected void onDestroy() {}
}`)
}

func TestARCoreReturnsLatestPoseWithUnchangedInstruction(t *testing.T) {
	source, err := renderarcore.Sources.ReadFile("ARSessionActivity.java")
	if err != nil {
		t.Fatal(err)
	}
	update := androidJavaSection(t, string(source), "    void updatePlacement(", "    void fail(")
	settle := androidJavaSection(t, string(source), "    private void settle(", "\n}")
	runAndroidJavaFixture(t, "ARPlacement", `import java.util.ArrayDeque;

class ARPlacement {
    static final int RESULT_OK = -1;
    static class Intent {
        float[] position;
        Intent putExtra(String key,String value) { return this; }
        Intent putExtra(String key,float[] value) { position=value; return this; }
    }
    static class TextView { void setText(String text) {} }
    static class Button { boolean enabled; void setEnabled(boolean value) { enabled=value; } }
    final ArrayDeque<Runnable> posted=new ArrayDeque<>();
    final TextView instruction=new TextView();
    final Button done=new Button();
    boolean finished;
    volatile float[] position;
    String reportedInstruction;
    Intent result;
    boolean isFinishing() { return finished; }
    boolean isDestroyed() { return false; }
    void runOnUiThread(Runnable callback) { posted.add(callback); }
    void setResult(int code,Intent data) { result=data; }
    void finish() { finished=true; }
`+update+settle+`
    public static void main(String[] args) {
        ARPlacement activity=new ARPlacement();
        activity.updatePlacement("Anchor placed",new float[]{1,2,3});
        activity.posted.remove().run();
        if (!activity.done.enabled) throw new AssertionError("Done not enabled");
        float[] latest={4,5,6};
        activity.updatePlacement("Anchor placed",latest);
        activity.settle("completed",null);
        if (activity.result==null || activity.result.position!=latest || !activity.posted.isEmpty()) throw new AssertionError("stale pose or extra UI work");
        ARPlacement lost=new ARPlacement();
        lost.updatePlacement("Anchor placed",latest);
        lost.posted.remove().run();
        lost.updatePlacement("Look around for a flat surface",null);
        lost.settle("completed",null);
        lost.posted.remove().run();
        if (lost.finished || lost.done.enabled) throw new AssertionError("completed without tracking");
    }
}`)
}

func TestARCoreCompilesAgainstSDKAndGeneratedBindings(t *testing.T) {
	aar := os.Getenv("GOFASTR_ARCORE_AAR")
	if aar == "" {
		t.Skip("GOFASTR_ARCORE_AAR must point to the opted-in ARCore AAR")
	}
	sdk, err := findAndroidSDK(false)
	if err != nil {
		t.Skipf("Android SDK/JDK unavailable: %v", err)
	}
	a := nativeApp(t).forAndroid(sdk.api)
	a.ARCoreAAR = aar
	dir, sources := androidHostSources(t, &a)
	r := &runner{ctx: context.Background(), out: &bytes.Buffer{}}
	jni := filepath.Join(dir, "jni")
	if err := os.MkdirAll(jni, 0o700); err != nil {
		t.Fatal(err)
	}
	jars, err := prepareARCore(r, &a, dir, jni)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkAARABIs(jni, a.ABIs); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "AndroidManifest.xml")
	if err := writeRendered("templates/android/AndroidManifest.xml.tmpl", manifest, a); err != nil {
		t.Fatal(err)
	}
	for _, component := range []string{"com.google.ar.core.min_apk_version", "com.google.ar.core.InstallActivity"} {
		if !strings.Contains(a.ARCoreComponents, component) {
			t.Errorf("SDK manifest component %q missing", component)
		}
	}
	resources, err := compileARCoreResources(r, sdk, dir)
	if err != nil {
		t.Fatal(err)
	}
	generated, _, err := linkAndroidLibraryResources(r, sdk, a, dir, manifest, append([]string{"--auto-add-overlay"}, resources...), []string{"com.google.ar.core"})
	if err != nil {
		t.Fatal(err)
	}
	classes := filepath.Join(dir, "classes")
	classpath := strings.Join(append([]string{sdk.androidJar}, jars...), string(os.PathListSeparator))
	args := append([]string{"--release", "11", "-encoding", "UTF-8", "-classpath", classpath, "-d", classes}, sources...)
	args = append(args, generated...)
	if output, err := exec.Command(sdk.javac, args...).CombinedOutput(); err != nil {
		t.Fatalf("ARCore SDK compile: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(classes, "dev", "gofastr", "nativeui", "arcore", "ARSessionActivity.class")); err != nil {
		t.Fatal(err)
	}
}
