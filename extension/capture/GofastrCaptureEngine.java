package dev.gofastr.nativeui;

import android.Manifest;
import android.content.Context;
import android.content.pm.PackageInfo;
import android.content.pm.PackageManager;
import android.graphics.SurfaceTexture;
import android.hardware.camera2.CameraAccessException;
import android.hardware.camera2.CameraCaptureSession;
import android.hardware.camera2.CameraCharacteristics;
import android.hardware.camera2.CameraDevice;
import android.hardware.camera2.CameraManager;
import android.hardware.camera2.CaptureRequest;
import android.hardware.camera2.params.StreamConfigurationMap;
import android.hardware.display.DisplayManager;
import android.media.MediaCodec;
import android.media.MediaCodecInfo;
import android.media.MediaFormat;
import android.media.MediaMuxer;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.SystemClock;
import android.util.Log;
import android.util.Range;
import android.util.Size;
import android.view.Display;
import android.view.Surface;
import org.json.JSONObject;
import java.io.File;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;
import java.util.concurrent.CopyOnWriteArrayList;

/**
 * One camera, encoder and recorder per process. The foreground service keeps
 * a session alive; preview surfaces come and go with the screen. Every field
 * is owned by the engine thread; the volatile ones are read by the UI.
 */
final class GofastrCaptureEngine {
    private static final String TAG = "gofastr-capture";
    private static GofastrCaptureEngine instance;

    static synchronized GofastrCaptureEngine get(Context context) {
        if (instance == null) instance = new GofastrCaptureEngine(context.getApplicationContext());
        return instance;
    }

    interface Ended { void ended(); }

    private final Context context;
    private final Handler handler;
    private final CopyOnWriteArrayList<Runnable> observers = new CopyOnWriteArrayList<>();
    private final Object muxerLock = new Object();
    private boolean attached;

    // Camera.
    private CameraDevice camera;
    private String cameraFacing = "", wantedFacing = "";
    private CameraCaptureSession cameraSession;
    private int configuration;
    private boolean opening;
    volatile Size size;
    volatile int sensorOrientation;
    volatile boolean frontFacing;
    private boolean realtimeClock;
    private long clockOffset;
    private Range<Integer> fpsRange;

    // Preview.
    private SurfaceTexture previewTexture;
    private Surface previewSurface;
    private boolean previewWanted;

    // Session.
    volatile String sessionId;
    volatile String status = "";
    volatile long startedAt;
    private JSONObject config;
    private Ended ended;
    private boolean recording, streaming, stopping;
    private MediaCodec video;
    private Surface encoderSurface;
    private byte[] parameterSets = new byte[0];
    private MediaMuxer muxer;
    /** The session's directory; each segment is one MP4 in it. */
    private File directory;
    private int muxVideo = -1, muxAudio = -1, segmentIndex, rotationHint;
    private boolean muxStarted, waitForAudioTrack, muxKeySeen, cutPending, muxFailed;
    /** False once the microphone ended early: later segments get no audio track. */
    private boolean audioAlive;
    /** Device clock of the current segment's first and last video frame. */
    private long segmentFirst = -1, segmentLast;
    /** Track formats, kept to open the next segment's muxer. */
    private MediaFormat videoTrackFormat, audioTrackFormat;
    private GofastrCaptureAudio audio;
    private GofastrCaptureSensors sensors;
    /** Encoded video so far, reported to Go with each heartbeat. Engine thread only. */
    private long encodedFrames, encodedBytes;
    private final Runnable heartbeat = new Runnable() {
        @Override public void run() {
            String id = sessionId;
            if (id == null || stopping) return;
            directives(event(id, "heartbeat", "{\"frames\":" + encodedFrames + ",\"bytes\":" + encodedBytes + "}"));
            handler.postDelayed(this, 1000);
        }
    };

    private final Runnable finalizeTimeout = this::finalizeRecording;

    /** Starts the muxer without audio when the AAC format is late, so video is never lost to the microphone. */
    private final Runnable audioTimeout = () -> {
        synchronized (muxerLock) {
            if (!waitForAudioTrack || muxer == null || muxStarted) return;
            Log.w(TAG, "no audio format in time; recording without audio");
            waitForAudioTrack = false;
            audioAlive = false;
            startMuxerLocked();
        }
    };

    private GofastrCaptureEngine(Context context) {
        this.context = context;
        HandlerThread thread = new HandlerThread("gofastr-capture");
        thread.start();
        handler = new Handler(thread.getLooper());
    }

    // ---- Shared helpers -------------------------------------------------

    /** Tells Go where recordings live. Safe to call repeatedly. */
    synchronized void attachStorage() {
        if (attached) return;
        try {
            capturebind.Capturebind.attach(root().getAbsolutePath());
            attached = true;
        } catch (Exception e) {
            Log.w(TAG, "capture storage unavailable", e);
        }
    }

    File root() { return new File(context.getFilesDir(), "gofastr-capture"); }

    boolean hasCamera() { return context.getPackageManager().hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY); }

    static boolean declared(Context context, String permission) {
        try {
            PackageInfo info = context.getPackageManager().getPackageInfo(context.getPackageName(), PackageManager.GET_PERMISSIONS);
            return info.requestedPermissions != null && Arrays.asList(info.requestedPermissions).contains(permission);
        } catch (PackageManager.NameNotFoundException e) {
            return false;
        }
    }

    boolean granted(String permission) {
        return Build.VERSION.SDK_INT < 23 || context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED;
    }

    void observe(Runnable observer) { observers.add(observer); }
    void unobserve(Runnable observer) { observers.remove(observer); }
    private void notifyObservers() { for (Runnable observer : observers) observer.run(); }

    private static String event(String id, String kind, String payload) {
        try {
            return capturebind.Capturebind.event(id, kind, payload);
        } catch (Exception e) {
            return "";
        }
    }

    private static String quote(String text) { return JSONObject.quote(text == null ? "" : text); }

    // ---- Preview --------------------------------------------------------

    /** Shows the camera in a texture. Without a session it opens the camera for preview only. */
    void attachPreview(SurfaceTexture texture, String facing) {
        handler.post(() -> {
            if (previewTexture != texture) {
                if (previewSurface != null) previewSurface.release();
                previewTexture = texture;
                previewSurface = null;
            }
            previewWanted = true;
            if (sessionId == null) ensureCamera(facing);
            else reconfigure();
        });
    }

    /**
     * Stops drawing into a texture. With release set, the engine owns the
     * texture and releases it once the camera no longer targets it: Android
     * destroys preview textures whenever the window stops, including with the
     * screen off, and a recording must outlive that.
     */
    void detachPreview(SurfaceTexture texture, boolean release) {
        handler.post(() -> {
            if (previewTexture == texture) {
                previewWanted = false;
                previewTexture = null;
                if (previewSurface != null) previewSurface.release();
                previewSurface = null;
                if (sessionId == null) closeCamera();
                else reconfigure();
            }
            if (release) texture.release();
        });
    }

    /** Drops a preview the system already destroyed, keeping the session. */
    private void dropPreview() {
        previewWanted = false;
        previewTexture = null;
        if (previewSurface != null) previewSurface.release();
        previewSurface = null;
    }

    private boolean previewUsable() {
        if (!previewWanted || previewTexture == null) return false;
        if (Build.VERSION.SDK_INT >= 26 && previewTexture.isReleased()) {
            dropPreview();
            return false;
        }
        return true;
    }

    // ---- Session --------------------------------------------------------

    /** A start that waits for the session it replaces to finish. Engine thread only. */
    private String queuedId;
    private JSONObject queuedConfig;
    private Ended queuedEnded;

    void startSession(String id, JSONObject config, Ended ended) {
        handler.post(() -> {
            if (id.equals(sessionId) || id.equals(queuedId)) return;
            if (sessionId != null) {
                // Go stops a pending session at once, so the app may start the
                // next before the device has heard. Ask Go about this one.
                String reply = stopping ? "stop" : event(sessionId, "heartbeat", "{\"frames\":" + encodedFrames + ",\"bytes\":" + encodedBytes + "}");
                directives(reply);
                if (!java.util.Arrays.asList(reply.split(",")).contains("stop")) {
                    event(id, "failed", "{\"reason\":\"busy\",\"message\":\"Another capture session is running.\"}");
                    ended.ended();
                    return;
                }
                if (queuedId != null) {
                    event(queuedId, "failed", "{\"reason\":\"busy\",\"message\":\"Another capture session replaced this one.\"}");
                    queuedEnded.ended();
                }
                queuedId = id;
                queuedConfig = config;
                queuedEnded = ended;
                return;
            }
            attachStorage();
            if (!granted(Manifest.permission.CAMERA)) {
                event(id, "failed", "{\"reason\":\"denied\",\"message\":\"Camera permission was denied.\"}");
                ended.ended();
                return;
            }
            this.sessionId = id;
            this.config = config;
            this.ended = ended;
            recording = config.optBoolean("record");
            streaming = config.optBoolean("stream");
            stopping = false;
            status = "Starting";
            notifyObservers();
            String facing = config.optString("camera", "back");
            if (camera != null && !facing.equals(cameraFacing)) closeCamera();
            if (camera == null) ensureCamera(facing);
            else beginSession();
        });
    }

    /** Stops the running session, if any. Reason is reported to Go. */
    void stopSession(String reason, String message) {
        handler.post(() -> finishSession("stopped", reason, message));
    }

    /** Called once the camera is open and a session is waiting. */
    private void beginSession() {
        String id = sessionId;
        try {
            int width = config.optInt("width", 1280), height = config.optInt("height", 720);
            int fps = config.optInt("frameRate", 30), bitrate = config.optInt("bitrate", 4_000_000);
            chooseFormat(width, height, fps);
            encodedFrames = encodedBytes = 0;
            video = createEncoder(size, fps, bitrate);
            encoderSurface = video.createInputSurface();
            video.setCallback(new MediaCodec.Callback() {
                @Override public void onInputBufferAvailable(MediaCodec codec, int index) {}
                @Override public void onOutputBufferAvailable(MediaCodec codec, int index, MediaCodec.BufferInfo info) { videoOutput(codec, index, info); }
                @Override public void onError(MediaCodec codec, MediaCodec.CodecException e) { finishSession("failed", "encoder", e.getMessage()); }
                @Override public void onOutputFormatChanged(MediaCodec codec, MediaFormat format) { videoFormat(format); }
            }, handler);
            video.start();
            int rotation = rotation();
            boolean wantAudio = config.optBoolean("audio") && declared(context, Manifest.permission.RECORD_AUDIO) && granted(Manifest.permission.RECORD_AUDIO);
            if (recording) {
                directory = new File(root(), id);
                if (!directory.isDirectory() && !directory.mkdirs()) throw new IllegalStateException("Cannot create the recording directory");
                rotationHint = rotation;
                segmentIndex = 1;
                cutPending = false;
                videoTrackFormat = audioTrackFormat = null;
                openMuxer();
            }
            boolean audioStarted = false;
            if (wantAudio) {
                audio = new GofastrCaptureAudio(audioOutput(id), realtimeClock);
                try {
                    audioStarted = audio.start(recording, streaming);
                } catch (Exception e) {
                    Log.w(TAG, "audio unavailable", e);
                }
                if (!audioStarted) audio = null;
            }
            synchronized (muxerLock) {
                audioAlive = audioStarted;
                waitForAudioTrack = audioStarted && recording;
            }
            boolean[] started = {false, false, false};
            boolean acceleration = config.optBoolean("acceleration");
            if (config.optBoolean("location") || config.optBoolean("motion") || config.optBoolean("gyroscope") || acceleration) {
                sensors = new GofastrCaptureSensors(context, frontFacing, (kind, payload) -> directives(event(id, kind, payload)));
                started = sensors.start(config.optBoolean("location"), config.optBoolean("motion"), config.optBoolean("gyroscope"), acceleration, config.optInt("motionRate", 30));
            }
            reconfigure();
            startedAt = SystemClock.elapsedRealtime();
            status = recording && streaming ? "Recording and streaming" : recording ? "Recording" : "Streaming";
            String report = "{\"width\":" + size.getWidth() + ",\"height\":" + size.getHeight() + ",\"rotation\":" + rotation
                + ",\"audio\":" + audioStarted + ",\"location\":" + started[0] + ",\"motion\":" + started[1] + ",\"acceleration\":" + started[2]
                + ",\"clock\":" + SystemClock.elapsedRealtimeNanos() + ",\"wall\":" + quote(GofastrCaptureSensors.iso(System.currentTimeMillis())) + "}";
            directives(event(id, "started", report));
            handler.postDelayed(heartbeat, 1000);
            notifyObservers();
        } catch (Exception e) {
            Log.w(TAG, "capture failed to start", e);
            finishSession("failed", "start", e.getMessage());
        }
    }

    private GofastrCaptureAudio.Output audioOutput(String id) {
        return new GofastrCaptureAudio.Output() {
            @Override public void aacFormat(MediaFormat format) {
                synchronized (muxerLock) {
                    audioTrackFormat = format;
                    if (muxer == null || muxStarted) return;
                    muxAudio = muxer.addTrack(format);
                    waitForAudioTrack = false;
                    startMuxerLocked();
                }
            }
            @Override public void aac(ByteBuffer data, MediaCodec.BufferInfo info) {
                synchronized (muxerLock) {
                    if (muxer != null && muxStarted && !muxFailed && muxAudio >= 0 && muxKeySeen) writeLocked(muxAudio, data, info);
                }
            }
            @Override public void ended() {
                // The microphone stopped, on request or because it failed.
                if (!id.equals(sessionId)) return;
                synchronized (muxerLock) {
                    audioAlive = false;
                    if (waitForAudioTrack && muxer != null && !muxStarted) {
                        waitForAudioTrack = false;
                        startMuxerLocked();
                    }
                }
            }
            @Override public void opus(byte[] data, long clock) {
                if (!streaming) return;
                try {
                    directives(capturebind.Capturebind.sample(id, 1, 1, clock + clockOffset, data));
                } catch (Exception ignored) {}
            }
        };
    }

    private void videoFormat(MediaFormat format) {
        synchronized (muxerLock) {
            videoTrackFormat = format;
            if (muxer == null || muxStarted) return;
            muxVideo = muxer.addTrack(format);
            if (waitForAudioTrack) handler.postDelayed(audioTimeout, 3000);
            startMuxerLocked();
        }
    }

    private File segmentFile(int index) { return new File(directory, String.format(java.util.Locale.US, "segment-%05d.mp4", index)); }

    /** Opens the muxer for segmentIndex. Its tracks come later. */
    private void openMuxer() throws java.io.IOException {
        muxer = new MediaMuxer(segmentFile(segmentIndex).getAbsolutePath(), MediaMuxer.OutputFormat.MUXER_OUTPUT_MPEG_4);
        muxer.setOrientationHint(rotationHint);
        muxVideo = muxAudio = -1;
        muxStarted = muxKeySeen = muxFailed = false;
        segmentFirst = -1;
    }

    /** The segment report for Go. A failed segment is reported too, so Go moves on to the next. */
    private String segmentReport(boolean failed) {
        return "{\"index\":" + segmentIndex + ",\"start\":" + segmentFirst + ",\"end\":" + segmentLast + (failed ? ",\"failed\":true" : "") + "}";
    }

    /**
     * Writes one sample. A failed write, such as a full disk, ends the
     * session: the muxer cannot finish this file, and Go deletes it. Hold
     * muxerLock.
     */
    private void writeLocked(int track, ByteBuffer data, MediaCodec.BufferInfo info) {
        try {
            muxer.writeSampleData(track, data, info);
        } catch (Exception e) {
            Log.w(TAG, "segment write failed", e);
            muxFailed = true;
            String message = e.getMessage();
            handler.post(() -> finishSession("failed", "storage", "The recording could not be written: " + message));
        }
    }

    /**
     * Finishes the current segment file and opens the next with the same
     * tracks, so the keyframe being written starts it. Returns the finished
     * segment's report for Go, or null. Hold muxerLock.
     */
    private String rotateLocked() {
        cutPending = false;
        String report = segmentReport(false);
        try {
            muxer.stop();
        } catch (Exception e) {
            Log.w(TAG, "segment did not finalize", e);
            report = segmentReport(true);
        }
        muxer.release();
        muxer = null;
        muxStarted = false;
        segmentIndex++;
        try {
            openMuxer();
            muxVideo = muxer.addTrack(videoTrackFormat);
            if (audioTrackFormat != null && audioAlive) muxAudio = muxer.addTrack(audioTrackFormat);
            muxer.start();
            muxStarted = true;
        } catch (Exception e) {
            Log.w(TAG, "next segment failed", e);
            if (muxer != null) { muxer.release(); muxer = null; }
            String message = e.getMessage();
            handler.post(() -> finishSession("failed", "storage", "The next segment could not be created: " + message));
        }
        return report;
    }

    private void startMuxerLocked() {
        if (muxStarted || muxVideo < 0 || waitForAudioTrack) return;
        muxer.start();
        muxStarted = true;
        requestKeyframe();
    }

    private void videoOutput(MediaCodec codec, int index, MediaCodec.BufferInfo info) {
        if (codec != video) return;
        ByteBuffer data;
        try {
            data = codec.getOutputBuffer(index);
        } catch (IllegalStateException e) {
            return;
        }
        boolean config = (info.flags & MediaCodec.BUFFER_FLAG_CODEC_CONFIG) != 0;
        boolean key = (info.flags & MediaCodec.BUFFER_FLAG_KEY_FRAME) != 0;
        boolean end = (info.flags & MediaCodec.BUFFER_FLAG_END_OF_STREAM) != 0;
        if (data != null && info.size > 0) {
            data.position(info.offset).limit(info.offset + info.size);
            if (config) {
                parameterSets = new byte[info.size];
                data.get(parameterSets);
            } else {
                encodedFrames++;
                encodedBytes += info.size;
                String finished = null;
                synchronized (muxerLock) {
                    if (cutPending && key && muxer != null && muxStarted && muxKeySeen && !muxFailed) finished = rotateLocked();
                    if (muxer != null && muxStarted && !muxFailed && (muxKeySeen || key)) {
                        muxKeySeen = true;
                        long at = info.presentationTimeUs * 1000 + clockOffset;
                        if (segmentFirst < 0) segmentFirst = at;
                        segmentLast = at;
                        writeLocked(muxVideo, data, info);
                    }
                }
                if (finished != null && sessionId != null) directives(event(sessionId, "segment", finished));
                if (streaming && sessionId != null) {
                    data.position(info.offset);
                    byte[] bytes = new byte[(key ? parameterSets.length : 0) + info.size];
                    if (key) System.arraycopy(parameterSets, 0, bytes, 0, parameterSets.length);
                    data.get(bytes, key ? parameterSets.length : 0, info.size);
                    try {
                        directives(capturebind.Capturebind.sample(sessionId, 0, key ? 1 : 0, info.presentationTimeUs * 1000 + clockOffset, bytes));
                    } catch (Exception ignored) {}
                }
            }
        }
        try {
            codec.releaseOutputBuffer(index, false);
        } catch (IllegalStateException ignored) {}
        if (end) finalizeRecording();
    }

    /** Applies "stop", "keyframe", "segment" and "bitrate:N" from Go. Any thread. */
    private void directives(String list) {
        if (list == null || list.isEmpty()) return;
        for (String directive : list.split(",")) {
            if (directive.equals("stop")) {
                handler.post(() -> finishSession("stopped", "requested", ""));
            } else if (directive.equals("keyframe")) {
                handler.post(this::requestKeyframe);
            } else if (directive.equals("segment")) {
                // Cut at the next keyframe, and ask for one now.
                handler.post(() -> {
                    if (muxer == null || stopping) return;
                    cutPending = true;
                    requestKeyframe();
                });
            } else if (directive.startsWith("bitrate:")) {
                int bitrate = Integer.parseInt(directive.substring(8));
                handler.post(() -> {
                    if (video == null) return;
                    Bundle parameters = new Bundle();
                    parameters.putInt(MediaCodec.PARAMETER_KEY_VIDEO_BITRATE, bitrate);
                    try { video.setParameters(parameters); } catch (IllegalStateException ignored) {}
                });
            }
        }
    }

    private void requestKeyframe() {
        if (video == null) return;
        Bundle parameters = new Bundle();
        parameters.putInt(MediaCodec.PARAMETER_KEY_REQUEST_SYNC_FRAME, 0);
        try { video.setParameters(parameters); } catch (IllegalStateException ignored) {}
    }

    /** Locks the recording around now, from the notification. */
    void lock() {
        handler.post(() -> {
            String id = sessionId;
            if (id != null && !stopping) directives(event(id, "lock", "{\"reason\":\"manual\"}"));
        });
    }

    private String endState = "stopped", endReason = "", endMessage = "";

    /** Starts an orderly stop: sensors and audio first, then video end of stream. */
    private void finishSession(String state, String reason, String message) {
        if (sessionId == null || stopping) return;
        stopping = true;
        endState = state;
        endReason = reason == null ? "" : reason;
        endMessage = message == null ? "" : message;
        handler.removeCallbacks(heartbeat);
        status = "Stopping";
        notifyObservers();
        if (sensors != null) { sensors.stop(); sensors = null; }
        if (audio != null) { audio.stop(); audio = null; }
        // Detach the encoder from the camera before ending its stream.
        reconfigure();
        if (video == null) {
            finalizeRecording();
            return;
        }
        try {
            video.signalEndOfInputStream();
            handler.postDelayed(finalizeTimeout, 3000);
        } catch (IllegalStateException e) {
            finalizeRecording();
        }
    }

    private void finalizeRecording() {
        String id = sessionId;
        if (id == null) return;
        handler.removeCallbacks(heartbeat);
        handler.removeCallbacks(finalizeTimeout);
        handler.removeCallbacks(audioTimeout);
        sessionId = null;
        if (video != null) {
            try { video.stop(); } catch (Exception ignored) {}
            video.release();
            video = null;
        }
        if (encoderSurface != null) { encoderSurface.release(); encoderSurface = null; }
        String finished = null;
        synchronized (muxerLock) {
            if (muxer != null) {
                boolean wrote = muxStarted && muxKeySeen && !muxFailed;
                try {
                    if (muxStarted) muxer.stop();
                } catch (Exception e) {
                    Log.w(TAG, "recording did not finalize", e);
                    wrote = false;
                }
                muxer.release();
                if (wrote) {
                    finished = segmentReport(false);
                } else {
                    segmentFile(segmentIndex).delete();
                    finished = segmentReport(true);
                }
                muxer = null;
            }
            cutPending = false;
        }
        status = "";
        if (finished != null) event(id, "segment", finished);
        event(id, endState, "{\"reason\":" + quote(endReason) + ",\"message\":" + quote(endMessage) + "}");
        Ended done = ended;
        ended = null;
        config = null;
        stopping = false;
        if (previewWanted && previewTexture != null) reconfigure();
        else closeCamera();
        if (done != null) done.ended();
        notifyObservers();
        if (queuedId != null) {
            String next = queuedId;
            JSONObject nextConfig = queuedConfig;
            Ended nextEnded = queuedEnded;
            queuedId = null;
            queuedConfig = null;
            queuedEnded = null;
            startSession(next, nextConfig, nextEnded);
        }
    }

    // ---- Camera ---------------------------------------------------------

    private void ensureCamera(String facing) {
        wantedFacing = facing;
        if (camera != null && facing.equals(cameraFacing)) {
            reconfigure();
            return;
        }
        // The open in flight checks wantedFacing when it lands.
        if (opening) return;
        closeCamera();
        if (!granted(Manifest.permission.CAMERA)) return;
        CameraManager manager = (CameraManager) context.getSystemService(Context.CAMERA_SERVICE);
        try {
            String chosen = null;
            for (String id : manager.getCameraIdList()) {
                Integer lens = manager.getCameraCharacteristics(id).get(CameraCharacteristics.LENS_FACING);
                boolean front = lens != null && lens == CameraCharacteristics.LENS_FACING_FRONT;
                if (front == facing.equals("front")) { chosen = id; break; }
            }
            if (chosen == null && manager.getCameraIdList().length > 0) chosen = manager.getCameraIdList()[0];
            if (chosen == null) throw new IllegalStateException("No camera");
            CameraCharacteristics characteristics = manager.getCameraCharacteristics(chosen);
            Integer orientation = characteristics.get(CameraCharacteristics.SENSOR_ORIENTATION);
            Integer lens = characteristics.get(CameraCharacteristics.LENS_FACING);
            Integer source = characteristics.get(CameraCharacteristics.SENSOR_INFO_TIMESTAMP_SOURCE);
            sensorOrientation = orientation == null ? 90 : orientation;
            frontFacing = lens != null && lens == CameraCharacteristics.LENS_FACING_FRONT;
            realtimeClock = source != null && source == CameraCharacteristics.SENSOR_INFO_TIMESTAMP_SOURCE_REALTIME;
            clockOffset = realtimeClock ? 0 : SystemClock.elapsedRealtimeNanos() - System.nanoTime();
            if (size == null || sessionId == null) chooseFormat(1280, 720, 30);
            cameraFacing = facing;
            opening = true;
            final String cameraId = chosen;
            manager.openCamera(cameraId, new CameraDevice.StateCallback() {
                @Override public void onOpened(CameraDevice device) {
                    opening = false;
                    camera = device;
                    cameraFacing = facing;
                    if (sessionId == null && !previewWanted) {
                        // The preview went away while the camera opened.
                        closeCamera();
                        return;
                    }
                    if (!facing.equals(wantedFacing)) {
                        closeCamera();
                        ensureCamera(wantedFacing);
                        return;
                    }
                    if (sessionId != null && video == null) beginSession();
                    else reconfigure();
                    notifyObservers();
                }
                @Override public void onDisconnected(CameraDevice device) {
                    opening = false;
                    device.close();
                    if (camera == device) camera = null;
                    finishSession("failed", "camera", "Another app took the camera.");
                }
                @Override public void onError(CameraDevice device, int error) {
                    opening = false;
                    device.close();
                    if (camera == device) camera = null;
                    finishSession("failed", "camera", "The camera reported error " + error + ".");
                }
            }, handler);
        } catch (CameraAccessException | SecurityException | IllegalStateException e) {
            opening = false;
            Log.w(TAG, "camera unavailable", e);
            finishSession("failed", "camera", e.getMessage());
        }
    }

    private void closeCamera() {
        configuration++;
        if (cameraSession != null) { try { cameraSession.close(); } catch (Exception ignored) {} cameraSession = null; }
        if (camera != null) { camera.close(); camera = null; }
        cameraFacing = "";
        notifyObservers();
    }

    /** Points the camera at the encoder and/or preview, whichever exist now. */
    private void reconfigure() {
        if (camera == null) return;
        List<Surface> targets = new ArrayList<>();
        if (encoderSurface != null && !stopping) targets.add(encoderSurface);
        final boolean encoding = !targets.isEmpty();
        final boolean previewing = previewUsable();
        if (previewing) {
            previewTexture.setDefaultBufferSize(size.getWidth(), size.getHeight());
            if (previewSurface == null) previewSurface = new Surface(previewTexture);
            targets.add(previewSurface);
        }
        final int generation = ++configuration;
        if (cameraSession != null) { try { cameraSession.close(); } catch (Exception ignored) {} cameraSession = null; }
        if (targets.isEmpty()) return;
        try {
            camera.createCaptureSession(targets, new CameraCaptureSession.StateCallback() {
                @Override public void onConfigured(CameraCaptureSession session) {
                    if (generation != configuration || camera == null) { session.close(); return; }
                    cameraSession = session;
                    try {
                        CaptureRequest.Builder request = camera.createCaptureRequest(encoding ? CameraDevice.TEMPLATE_RECORD : CameraDevice.TEMPLATE_PREVIEW);
                        for (Surface target : targets) request.addTarget(target);
                        if (fpsRange != null) request.set(CaptureRequest.CONTROL_AE_TARGET_FPS_RANGE, fpsRange);
                        request.set(CaptureRequest.CONTROL_MODE, CaptureRequest.CONTROL_MODE_AUTO);
                        request.set(CaptureRequest.CONTROL_AF_MODE, CaptureRequest.CONTROL_AF_MODE_CONTINUOUS_VIDEO);
                        session.setRepeatingRequest(request.build(), null, handler);
                    } catch (CameraAccessException | IllegalStateException e) {
                        finishSession("failed", "camera", e.getMessage());
                    }
                }
                @Override public void onConfigureFailed(CameraCaptureSession session) {
                    if (generation != configuration) return;
                    if (withoutPreview(previewing, encoding)) return;
                    finishSession("failed", "camera", "The camera rejected the output configuration.");
                }
            }, handler);
        } catch (CameraAccessException | IllegalStateException | IllegalArgumentException e) {
            if (withoutPreview(previewing, encoding)) return;
            finishSession("failed", "camera", e.getMessage());
        }
    }

    /**
     * Retries without the preview when a configuration that included it
     * failed: usually its texture died between the check and the camera call.
     * A preview problem never ends a recording.
     */
    private boolean withoutPreview(boolean previewing, boolean encoding) {
        if (!previewing) return false;
        Log.w(TAG, "camera configuration with preview failed; continuing without it");
        dropPreview();
        if (encoding) reconfigure();
        else if (sessionId == null) closeCamera();
        return true;
    }

    /** Picks the largest encoder-friendly size within the request and a frame rate range. */
    private void chooseFormat(int width, int height, int fps) throws CameraAccessException {
        CameraManager manager = (CameraManager) context.getSystemService(Context.CAMERA_SERVICE);
        String id = camera != null ? camera.getId() : null;
        if (id == null) {
            for (String candidate : manager.getCameraIdList()) {
                Integer lens = manager.getCameraCharacteristics(candidate).get(CameraCharacteristics.LENS_FACING);
                if ((lens != null && lens == CameraCharacteristics.LENS_FACING_FRONT) == frontFacing) { id = candidate; break; }
            }
        }
        if (id == null) id = manager.getCameraIdList()[0];
        CameraCharacteristics characteristics = manager.getCameraCharacteristics(id);
        StreamConfigurationMap map = characteristics.get(CameraCharacteristics.SCALER_STREAM_CONFIGURATION_MAP);
        Size best = new Size(width, height);
        if (map != null) {
            Size[] sizes = map.getOutputSizes(MediaCodec.class);
            double ratio = (double) width / height;
            long bestScore = Long.MAX_VALUE;
            for (Size candidate : sizes) {
                if (candidate.getWidth() > width || candidate.getHeight() > height || candidate.getWidth() % 16 != 0) continue;
                long area = (long) width * height - (long) candidate.getWidth() * candidate.getHeight();
                long penalty = Math.abs((double) candidate.getWidth() / candidate.getHeight() - ratio) > 0.01 ? (long) width * height : 0;
                if (area + penalty < bestScore) { bestScore = area + penalty; best = candidate; }
            }
            if (bestScore == Long.MAX_VALUE && sizes.length > 0) best = sizes[sizes.length - 1];
        }
        size = best;
        fpsRange = null;
        Range<Integer>[] ranges = characteristics.get(CameraCharacteristics.CONTROL_AE_AVAILABLE_TARGET_FPS_RANGES);
        if (ranges != null) {
            for (Range<Integer> range : ranges) {
                if (range.getUpper() != fps) continue;
                if (fpsRange == null || range.getLower() > fpsRange.getLower()) fpsRange = range;
            }
        }
    }

    private static MediaCodec createEncoder(Size size, int fps, int bitrate) throws Exception {
        MediaFormat format = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, size.getWidth(), size.getHeight());
        format.setInteger(MediaFormat.KEY_COLOR_FORMAT, MediaCodecInfo.CodecCapabilities.COLOR_FormatSurface);
        format.setInteger(MediaFormat.KEY_BIT_RATE, bitrate);
        format.setInteger(MediaFormat.KEY_FRAME_RATE, fps);
        format.setInteger(MediaFormat.KEY_I_FRAME_INTERVAL, 2);
        MediaCodec codec = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_VIDEO_AVC);
        // Baseline decodes in every WebRTC receiver; fall back if the encoder refuses it.
        MediaFormat baseline = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, size.getWidth(), size.getHeight());
        baseline.setInteger(MediaFormat.KEY_COLOR_FORMAT, MediaCodecInfo.CodecCapabilities.COLOR_FormatSurface);
        baseline.setInteger(MediaFormat.KEY_BIT_RATE, bitrate);
        baseline.setInteger(MediaFormat.KEY_FRAME_RATE, fps);
        baseline.setInteger(MediaFormat.KEY_I_FRAME_INTERVAL, 2);
        baseline.setInteger(MediaFormat.KEY_PROFILE, MediaCodecInfo.CodecProfileLevel.AVCProfileBaseline);
        baseline.setInteger(MediaFormat.KEY_LEVEL, MediaCodecInfo.CodecProfileLevel.AVCLevel31);
        try {
            codec.configure(baseline, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE);
        } catch (Exception e) {
            codec.reset();
            codec.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE);
        }
        return codec;
    }

    /** Clockwise degrees a player rotates the encoded picture to show it upright. */
    private int rotation() {
        int degrees = 0;
        DisplayManager displays = (DisplayManager) context.getSystemService(Context.DISPLAY_SERVICE);
        Display display = displays == null ? null : displays.getDisplay(Display.DEFAULT_DISPLAY);
        if (display != null) {
            switch (display.getRotation()) {
                case Surface.ROTATION_90: degrees = 90; break;
                case Surface.ROTATION_180: degrees = 180; break;
                case Surface.ROTATION_270: degrees = 270; break;
                default: break;
            }
        }
        return frontFacing ? (sensorOrientation + degrees) % 360 : (sensorOrientation - degrees + 360) % 360;
    }

    /** Display rotation in degrees, for preview transforms. */
    int displayRotation() {
        DisplayManager displays = (DisplayManager) context.getSystemService(Context.DISPLAY_SERVICE);
        Display display = displays == null ? null : displays.getDisplay(Display.DEFAULT_DISPLAY);
        return display == null ? 0 : display.getRotation();
    }
}
