"""Exercise URL handoff and App Link routing on an isolated Android 12+ emulator.

Install the device-features demo from docs/android.md first. This uses the OS
user-selection override for placeholder hosts; it does not test live Digital
Asset Links verification.
"""
from __future__ import annotations

import argparse
import re
import shlex
import time

from android_smoke import Android


LINK_ACTION = ["-a", "android.intent.action.VIEW", "-c", "android.intent.category.BROWSABLE"]
HOSTS = ("links.example.org", "links.example.net")
GUIDE = "https://developer.android.com/training/app-links"


def resumed_package(a):
    activities = a.adb("shell", "dumpsys", "activity", "activities").decode()
    activity = re.search(r"topResumedActivity=ActivityRecord\{\S+ u\d+ ([^/ ]+)/", activities)
    return activity[1] if activity else ""


def open_link(a, url, *options):
    a.adb("shell", "am", "start", "-W", *LINK_ACTION, "-d", shlex.quote(url), *options)
    assert resumed_package(a) == a.package, f"Link did not open the demo: {url}"


def find_source(a, source):
    a.find(f"Open a web URL with Android. Link source: {source}")


def smoke(a):
    component = a.package + "/.MainActivity"
    domains = a.adb("shell", "pm", "get-app-links", a.package).decode()
    assert all(f"{host}:" in domains for host in HOSTS), domains
    a.adb("shell", "pm", "set-app-links-user-selection", "--user", "0", "--package", a.package, "true", *HOSTS)
    try:
        a.adb("shell", "am", "force-stop", a.package)
        open_link(a, f"https://{HOSTS[0]}/unregistered")
        a.find("Device features")
        a.check("A cold link to an unknown Go route falls back to the app root")

        a.adb("shell", "am", "force-stop", a.package)
        open_link(a, f"https://{HOSTS[0]}/features/links?source=cold%20launch#section")
        find_source(a, "cold launch")
        a.check("Android routes an implicit cold HTTPS link, retaining query but not fragment")

        open_link(a, f"http://{HOSTS[1]}/features/links?source=warm%2Blaunch")
        find_source(a, "warm+launch")
        a.check("Android routes an implicit warm HTTP link from the second configured host")

        for invalid in (
            "https://unlisted.example.org/features/secure",
            f"https://{HOSTS[0]}:8443/features/secure",
            f"https://user@{HOSTS[0]}/features/secure",
        ):
            open_link(a, invalid, "-n", component, "--ez", "gofastr.reminder", "true",
                      "--es", "route", "/features/secure")
            find_source(a, "warm+launch")
        a.check("The activity ignores unlisted hosts, nonstandard ports and credentials")

        open_link(a, "gofastr-links-demo://features/links?source=custom")
        find_source(a, "custom")
        a.check("The custom URL scheme still opens Go routes")

        handler = a.adb("shell", "pm", "resolve-activity", "--brief", *LINK_ACTION, "-d", GUIDE).decode()
        handler_package = handler.strip().splitlines()[-1].split("/")[0]
        assert "/" in handler and handler_package != a.package, handler
        a.tap("native.action.open-web")
        deadline = time.monotonic() + 10
        while resumed_package(a) != handler_package:
            assert time.monotonic() < deadline, f"{handler_package} did not open"
            time.sleep(0.25)
        a.check("The Go action hands the HTTPS URL to Android's external handler")
    finally:
        a.adb("shell", "pm", "set-app-links-user-selection", "--user", "0", "--package", a.package, "false", *HOSTS)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--serial", required=True)
    parser.add_argument("--package", default="dev.gofastr.linksdemo")
    parser.add_argument("--out", default="dist/android-links-acceptance")
    args = parser.parse_args()
    android = Android(args.serial, args.package, args.out)
    try:
        smoke(android)
    except Exception:
        android.shot("failure")
        raise
