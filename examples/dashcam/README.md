# Dash cam

An Android dash cam on the native renderer. It records in short files,
deletes the oldest unlocked ones to stay within a loop size, and locks the
footage around an impact or a press of Lock. Recording continues with the
screen off. The capture library behind it is described in
[video capture](../../docs/capture.md).

```sh
go run ./cmd/gofastr-native run --platform android-emu --renderer native \
  --pkg ./examples/dashcam --name DashCam \
  --microphone-usage "Record sound in the car when you turn it on." --out dist/dashcam
```

The main screen shows the camera and Start. While recording it shows Lock
and Stop, the elapsed time and file number, position, speed, facing, the last
lock, and how much of the loop is used. The recording notification has Lock
and Stop too.

Locked clips lists every locked file, newest first. A clip shows where it
started and the strongest jolt in it, and can be unlocked, which hands it
back to loop recording, or deleted.

Settings, saved in `dashcam.json` in the app's data directory:

| Setting | Choices | Default |
| --- | --- | --- |
| Quality | 720p, 1080p | 1080p |
| File length | 1, 3 or 5 minutes | 1 minute |
| Loop size | 2 to 32 GB | 4 GB |
| Impact lock | off, 3 g, 2 g, 1.5 g | 2 g |
| Record sound | on, off | off |
| Record location | on, off | on |
| Start recording when the app opens | on, off | off |

Android does not let a camera foreground service start from the background
on Android 11 and newer. Start on open therefore begins when you open the
app, not on boot or when the phone joins the car's Bluetooth.

Locked clips do not count toward the loop size, so many locks can fill the
phone. Unlock or delete clips you no longer need.
