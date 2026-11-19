package dev.gofastr.nativeui;

import android.Manifest;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.os.Build;
import android.os.IBinder;
import android.os.PowerManager;
import android.util.Log;
import org.json.JSONObject;

/**
 * Keeps the process in the foreground while a capture session runs, so
 * recording and streaming continue with the screen off or another app open.
 * The notification's Stop action ends the session; Lock keeps the footage
 * around now from loop recording.
 */
public final class GofastrCaptureService extends Service {
    private static final String START = "dev.gofastr.capture.START", STOP = "dev.gofastr.capture.STOP", LOCK = "dev.gofastr.capture.LOCK";
    private static final String CHANNEL = "gofastr-capture";
    private static final int NOTIFICATION = 0x4743;
    private PowerManager.WakeLock wake;
    private String running;

    static void start(Context context, String session, JSONObject config) {
        Intent intent = new Intent(context, GofastrCaptureService.class).setAction(START)
            .putExtra("session", session).putExtra("config", config.toString());
        if (Build.VERSION.SDK_INT >= 26) context.startForegroundService(intent);
        else context.startService(intent);
    }

    @Override public IBinder onBind(Intent intent) { return null; }

    @Override public int onStartCommand(Intent intent, int flags, int startId) {
        String action = intent == null ? null : intent.getAction();
        if (STOP.equals(action)) {
            GofastrCaptureEngine.get(this).stopSession("user", "Stopped from the notification.");
            return START_NOT_STICKY;
        }
        if (LOCK.equals(action)) {
            GofastrCaptureEngine.get(this).lock();
            return START_NOT_STICKY;
        }
        if (!START.equals(action)) {
            // Restarted by the system without a session: nothing to resume.
            stopSelf();
            return START_NOT_STICKY;
        }
        String session = intent.getStringExtra("session");
        JSONObject config;
        try {
            config = new JSONObject(intent.getStringExtra("config"));
        } catch (Exception e) {
            // A service started with startForegroundService must reach
            // startForeground before it stops, or Android ends the app.
            try {
                foreground(new JSONObject());
            } catch (RuntimeException ignored) {}
            GofastrCaptureEngine.get(this).attachStorage();
            if (session != null) {
                try {
                    capturebind.Capturebind.event(session, "failed", "{\"reason\":\"protocol\",\"message\":\"The capture settings could not be read.\"}");
                } catch (Exception ignored) {}
            }
            if (Build.VERSION.SDK_INT >= 24) stopForeground(STOP_FOREGROUND_REMOVE);
            stopSelf();
            return START_NOT_STICKY;
        }
        GofastrCaptureEngine engine = GofastrCaptureEngine.get(this);
        try {
            foreground(config);
        } catch (RuntimeException e) {
            // Android 12+ refuses a foreground start from the background.
            Log.w("gofastr-capture", "foreground start refused", e);
            engine.attachStorage();
            try {
                capturebind.Capturebind.event(session, "failed", "{\"reason\":\"background\",\"message\":\"Android refused to start capture in the background.\"}");
            } catch (Exception ignored) {}
            stopSelf();
            return START_NOT_STICKY;
        }
        if (wake == null) {
            PowerManager power = (PowerManager) getSystemService(POWER_SERVICE);
            wake = power.newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "gofastr:capture");
            wake.setReferenceCounted(false);
        }
        wake.acquire(12 * 60 * 60 * 1000L);
        running = session;
        engine.startSession(session, config, () -> ended(session));
        return START_NOT_STICKY;
    }

    private void ended(String session) {
        new android.os.Handler(getMainLooper()).post(() -> {
            if (running != null && !running.equals(session)) return;
            running = null;
            if (wake != null && wake.isHeld()) wake.release();
            if (Build.VERSION.SDK_INT >= 24) stopForeground(STOP_FOREGROUND_REMOVE);
            stopSelf();
        });
    }

    @Override public void onDestroy() {
        if (wake != null && wake.isHeld()) wake.release();
        super.onDestroy();
    }

    private void foreground(JSONObject config) {
        NotificationManager manager = (NotificationManager) getSystemService(NOTIFICATION_SERVICE);
        Notification.Builder builder;
        if (Build.VERSION.SDK_INT >= 26) {
            manager.createNotificationChannel(new NotificationChannel(CHANNEL, "Camera capture", NotificationManager.IMPORTANCE_LOW));
            builder = new Notification.Builder(this, CHANNEL);
        } else {
            builder = new Notification.Builder(this);
        }
        int immutable = Build.VERSION.SDK_INT >= 23 ? PendingIntent.FLAG_IMMUTABLE : 0;
        PendingIntent stop = PendingIntent.getService(this, 1, new Intent(this, GofastrCaptureService.class).setAction(STOP), immutable | PendingIntent.FLAG_UPDATE_CURRENT);
        Intent launch = getPackageManager().getLaunchIntentForPackage(getPackageName());
        if (launch != null) builder.setContentIntent(PendingIntent.getActivity(this, 2, launch, immutable | PendingIntent.FLAG_UPDATE_CURRENT));
        String what = config.optBoolean("record") && config.optBoolean("stream") ? "Recording and streaming"
            : config.optBoolean("record") ? "Recording" : "Streaming";
        builder.setSmallIcon(android.R.drawable.presence_video_online)
            .setContentTitle(what + " video")
            .setContentText("Camera" + (config.optBoolean("location") ? ", location" : "") + (config.optBoolean("audio") ? " and microphone" : "") + " in use")
            .setOngoing(true);
        if (config.optBoolean("record")) {
            PendingIntent lock = PendingIntent.getService(this, 3, new Intent(this, GofastrCaptureService.class).setAction(LOCK), immutable | PendingIntent.FLAG_UPDATE_CURRENT);
            builder.addAction(new Notification.Action.Builder(null, "Lock", lock).build());
        }
        builder.addAction(new Notification.Action.Builder(null, "Stop", stop).build());
        Notification notification = builder.build();
        if (Build.VERSION.SDK_INT >= 30) {
            GofastrCaptureEngine engine = GofastrCaptureEngine.get(this);
            int types = ServiceInfo.FOREGROUND_SERVICE_TYPE_CAMERA;
            if (config.optBoolean("location") && (engine.granted(Manifest.permission.ACCESS_FINE_LOCATION) || engine.granted(Manifest.permission.ACCESS_COARSE_LOCATION))) {
                types |= ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION;
            }
            if (config.optBoolean("audio") && engine.granted(Manifest.permission.RECORD_AUDIO)) {
                types |= ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE;
            }
            startForeground(NOTIFICATION, notification, types);
        } else {
            startForeground(NOTIFICATION, notification);
        }
    }
}
