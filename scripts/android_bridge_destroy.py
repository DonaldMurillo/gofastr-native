#!/usr/bin/env python3
"""Instrument an existing WebView debug build to deliver a callback after destroy.

Build examples/notes/notesmobile with --renderer webview first, then run:
  python3 scripts/android_bridge_destroy.py dist/bridge-test
  adb install -r dist/bridge-test/bridge-destroy.apk
  adb logcat -c
  adb shell am start -n dev.gofastr.notesmobile/.MainActivity --ez bridge_destroy true
  adb logcat -d -s AndroidRuntime gofastr-native

The hook retains the real registered callback and invokes it on the UI thread
after onDestroy. It does not alter executor or lifecycle fencing behavior.
Without the intent extra, the instrumented app launches normally.
"""

import os
from pathlib import Path
import subprocess
import sys
import zipfile


def run(*args):
    subprocess.run([str(arg) for arg in args], check=True)


out = Path(sys.argv[1]).resolve()
sdk = Path(os.environ.get("ANDROID_HOME", os.environ.get("ANDROID_SDK_ROOT", "")))
if not sdk.is_dir() or not (sdk / "platforms").is_dir():
    raise SystemExit("Set ANDROID_HOME to the Android SDK directory")
tools = sorted((sdk / "build-tools").iterdir())[-1]
android = sorted((sdk / "platforms").glob("*/android.jar"))[-1]
work = out / "work"
host = work / "android"
source = next((host / "src").glob("**/MainActivity.java"))
original = source.read_text()
text = original.replace(
    "private WebView web;",
    "private WebMessagePort.WebMessageCallback lateCallback;\n    private WebView web;",
).replace(
    "super.onDestroy();",
    '''super.onDestroy();
        if (getIntent().getBooleanExtra("bridge_destroy", false)) {
            new android.os.Handler(getMainLooper()).postDelayed(() -> {
                Log.i(TAG, "repro: delivering callback after onDestroy");
                lateCallback.onMessage(null, new WebMessage("{}"));
            }, 100);
        }''',
).replace(
    "mine.setWebMessageCallback(new WebMessagePort.WebMessageCallback() {",
    "lateCallback = new WebMessagePort.WebMessageCallback() {",
).replace(
    '            view.postWebMessage(new WebMessage("gofastr-mobile-bridge",',
    '''            mine.setWebMessageCallback(lateCallback);
            if (getIntent().getBooleanExtra("bridge_destroy", false)) {
                new android.os.Handler(getMainLooper()).postDelayed(() -> finish(), 3000);
            }
            new android.os.Handler(getMainLooper()).postDelayed(() ->
                view.evaluateJavascript("fetch('/', {method:'POST', body:'bridge-probe'}).then(r => r.text()).then(t => console.log('bridge-probe reply ' + t.length))", null), 500);
            view.postWebMessage(new WebMessage("gofastr-mobile-bridge",''',
)
text = text.replace(
    '            });\n            mine.setWebMessageCallback(lateCallback);',
    '            };\n            mine.setWebMessageCallback(lateCallback);',
)
try:
    source.write_text(text)
    run("javac", "--release", "11", "-classpath", f"{android}:{work / 'aar/classes.jar'}",
        "-d", host / "classes", *sorted((host / "src").glob("**/*.java")))
    dex = host / "repro-dex"
    dex.mkdir(exist_ok=True)
    run(tools / "d8", "--release", "--min-api", "24", "--lib", android,
        "--output", dex, *sorted((host / "classes").glob("**/*.class")), work / "aar/classes.jar")
    unsigned = out / "bridge-destroy-unsigned.apk"
    with zipfile.ZipFile(host / "unsigned.apk") as src, zipfile.ZipFile(unsigned, "w") as dst:
        for entry in src.infolist():
            dst.writestr(entry, (dex / "classes.dex").read_bytes() if entry.filename == "classes.dex" else src.read(entry))
    aligned = out / "bridge-destroy-aligned.apk"
    run(tools / "zipalign", "-p", "-f", "4", unsigned, aligned)
    run(tools / "apksigner", "sign", "--ks", work / "debug.keystore",
        "--ks-pass", "pass:android", "--key-pass", "pass:android", "--out", out / "bridge-destroy.apk", aligned)
finally:
    source.write_text(original)
