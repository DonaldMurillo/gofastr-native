package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the template's callback with controllable worker and UI queues.
func TestAndroidWebViewBridgeAfterDestroy(t *testing.T) {
	javac, err := exec.LookPath("javac")
	if err != nil {
		t.Skip("JDK unavailable")
	}
	java, err := exec.LookPath("java")
	if err != nil {
		t.Skip("Java runtime unavailable")
	}
	data, err := os.ReadFile("templates/android/MainActivity.java.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	between := func(start, end string) string {
		t.Helper()
		_, tail, ok := strings.Cut(source, start)
		if !ok {
			t.Fatalf("template missing %q", start)
		}
		body, _, ok := strings.Cut(tail, end)
		if !ok {
			t.Fatalf("template missing %q after %q", end, start)
		}
		return body
	}
	if !strings.Contains(source, "private volatile boolean destroyed;") ||
		!strings.Contains(source, "if (destroyed || !trusted(Uri.parse(url)))") ||
		!strings.Contains(source, "private WebMessagePort bridgePort;") {
		t.Fatal("bridge lifecycle fencing missing")
	}
	callback := between("public void onMessage(WebMessagePort port, WebMessage message) {", "\n                }\n            });")
	callback = strings.ReplaceAll(callback, "{{.JavaClass}}", "Go")
	teardown := between("protected void onDestroy() {", "        if (fileChoice != null)")
	replacePort := between("final WebMessagePort mine = ports[0];", "            mine.setWebMessageCallback(")
	harness := `
import java.util.*;
import java.util.concurrent.RejectedExecutionException;
public class BridgeTest {
    static final String TAG = "test";
    boolean destroyed;
    final Worker bridgeThreads = new Worker();
    final List<Runnable> ui = new ArrayList<>();
    final WebMessagePort mine = new WebMessagePort();
    WebMessagePort bridgePort = mine;
    static class WebMessage {
        final String data;
        WebMessage(String data) { this.data = data; }
        String getData() { return data; }
    }
    static class WebMessagePort {
        boolean closed;
        int replies;
        void close() { closed = true; }
        void postMessage(WebMessage m) {
            if (closed) throw new AssertionError("reply to closed port");
            replies++;
        }
    }
    static class Worker {
        boolean rejected;
        Runnable pending;
        void execute(Runnable r) {
            if (rejected) throw new RejectedExecutionException();
            pending = r;
        }
    }
    static class Go {
        static int calls;
        static String bridge(String m) { calls++; return m; }
    }
    static class Log { static void d(String tag, String message) {} }
    void runOnUiThread(Runnable r) { ui.add(r); }
    String answer(String reply) { return reply; }
    void onMessage(WebMessagePort port, WebMessage message) { CALLBACK }
    void destroy() { TEARDOWN bridgeThreads.rejected = true; }
    void navigate(WebMessagePort mine) { REPLACE_PORT }
    static void check(boolean b) { if (!b) throw new AssertionError(); }
    public static void main(String[] args) {
        for (String input : new String[]{"request", ""}) {
            BridgeTest b = new BridgeTest();
            b.onMessage(b.mine, new WebMessage(input));
            b.bridgeThreads.pending.run();
            b.ui.remove(0).run();
            check(b.mine.replies == 1);
        }
        BridgeTest late = new BridgeTest();
        late.destroy();
        check(late.mine.closed && late.bridgePort == null);
        late.onMessage(late.mine, null);
        check(late.bridgeThreads.pending == null);
        BridgeTest rejected = new BridgeTest();
        rejected.bridgeThreads.rejected = true;
        rejected.onMessage(rejected.mine, new WebMessage("request"));
        check(rejected.bridgeThreads.pending == null);
        BridgeTest worker = new BridgeTest();
        worker.onMessage(worker.mine, new WebMessage("request"));
        int calls = Go.calls;
        worker.destroy();
        worker.bridgeThreads.pending.run();
        check(Go.calls == calls && worker.ui.isEmpty());
        BridgeTest reply = new BridgeTest();
        reply.onMessage(reply.mine, new WebMessage("request"));
        reply.bridgeThreads.pending.run();
        reply.destroy();
        reply.ui.remove(0).run();
        check(reply.mine.replies == 0);
        BridgeTest navigation = new BridgeTest();
        navigation.onMessage(navigation.mine, new WebMessage("request"));
        navigation.bridgeThreads.pending.run();
        WebMessagePort next = new WebMessagePort();
        navigation.navigate(next);
        check(navigation.mine.closed && navigation.bridgePort == next && !next.closed);
        navigation.ui.remove(0).run();
        check(navigation.mine.replies == 0 && next.replies == 0);
        navigation.destroy();
        check(next.closed && navigation.bridgePort == null);
        System.out.println("bridge lifecycle: normal, empty, destroyed, rejected, queued worker, queued reply and navigation OK");
    }
}`
	harness = strings.ReplaceAll(strings.ReplaceAll(harness, "CALLBACK", callback), "TEARDOWN", teardown)
	harness = strings.ReplaceAll(harness, "REPLACE_PORT", replacePort)
	dir := t.TempDir()
	path := filepath.Join(dir, "BridgeTest.java")
	if err := os.WriteFile(path, []byte(harness), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(javac, "--release", "11", path).CombinedOutput(); err != nil {
		t.Fatalf("compile bridge lifecycle harness: %v\n%s", err, output)
	}
	if output, err := exec.Command(java, "-cp", dir, "BridgeTest").CombinedOutput(); err != nil {
		t.Fatalf("bridge lifecycle: %v\n%s", err, output)
	} else {
		t.Log(string(output))
	}
}
