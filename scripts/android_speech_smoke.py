"""Exercise the speech demo on an isolated English-language Android emulator.

Requires a text-to-speech engine and recognizer. Resets only the demo's
microphone grant and denial flags, preserving app data.
"""
from __future__ import annotations

import argparse

from android_smoke import Android


def smoke(device: Android, no_mic: bool):
    permission = "android.permission.RECORD_AUDIO"
    if not no_mic:
        device.adb("shell", "pm", "revoke", device.package, permission)
        device.adb("shell", "pm", "clear-permission-flags", device.package, permission, "user-set", "user-fixed")
    device.adb("shell", "am", "force-stop", device.package)
    device.adb("shell", "am", "start", "-W", "-n", device.package + "/.MainActivity")
    device.find("Speech demo")
    device.tap("native.action.speak")
    device.find("Speech finished.", timeout=30)
    device.check("Android text-to-speech provider reports playback complete without a microphone grant")

    device.tap("native.action.recognize")
    if no_mic:
        device.find("Speech recognition is not available for this build or device.")
        device.check("Recognition is unavailable without microphone opt-in")
        return

    permission_id = "com.android.permissioncontroller:id/permission_"
    device.tap(permission_id + "deny_button")
    device.find("Permission was denied.")
    device.check("Android microphone denial settles without recording")

    device.tap("native.action.recognize")
    device.tap(permission_id + "allow_foreground_only_button")
    device.tap("android:id/button2")
    device.find("Speech cancelled.")
    device.check("User can cancel a live recognition session")

    device.tap("native.action.speak")
    device.find("Speech finished.", timeout=30)
    device.check("New speech operation succeeds after recognition cancellation")

    device.tap("native.action.recognize")
    device.find("Speech recognition")
    device.adb("shell", "input", "keyevent", "3")
    device.adb("shell", "am", "start", "-W", "-n", device.package + "/.MainActivity")
    device.find("Speech cancelled.")
    device.check("Backgrounding cancels recognition")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--serial", required=True)
    parser.add_argument("--package", default="dev.gofastr.speechnative")
    parser.add_argument("--out", default="dist/android-speech-acceptance")
    parser.add_argument("--no-mic", action="store_true", help="Demo was built without microphone opt-in.")
    args = parser.parse_args()
    android = Android(args.serial, args.package, args.out)
    try:
        smoke(android, args.no_mic)
    except Exception:
        android.shot("failure")
        raise
