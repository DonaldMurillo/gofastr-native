package dev.gofastr.nativeui.arcore;

import android.content.Intent;
import android.os.Handler;
import android.os.Looper;
import com.google.ar.core.ArCoreApk;
import dev.gofastr.nativeui.Extensions;
import dev.gofastr.nativeui.GofastrActivity;
import dev.gofastr.nativeui.JSON;
import dev.gofastr.nativeui.Services;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.Collections;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

public final class ARCore implements Extensions.ServiceFactory {
    public static void install() {
        Extensions.install("gofastr.arcore/v1", new ARCore());
    }

    @Override public Map<String,String> capabilities(GofastrActivity host) {
        return Collections.singletonMap("ar/v1", "available");
    }

    @Override public void perform(GofastrActivity host, JSONObject request, Consumer<JSONObject> result) {
        switch (request.optString("operation")) {
            case "check":
                check(host, result);
                break;
            case "place":
                host.deviceServices().launchExtensionActivity(new Intent(host, ARSessionActivity.class), data -> {
                    if (data == null) {
                        result.accept(JSON.object("status", "cancelled"));
                        return;
                    }
                    String status = data.getStringExtra("status");
                    if (!"completed".equals(status)) {
                        result.accept(Services.outcome(status, data.getStringExtra("message")));
                        return;
                    }
                    float[] position = data.getFloatArrayExtra("position");
                    if (position == null || position.length != 3) {
                        result.accept(Services.outcome("failed", "The anchor position was not returned."));
                        return;
                    }
                    JSONArray coordinates = new JSONArray();
                    for (float coordinate : position) {
                        if (!Float.isFinite(coordinate)) {
                            result.accept(Services.outcome("failed", "The anchor position was invalid."));
                            return;
                        }
                        coordinates.put(Float.valueOf(coordinate));
                    }
                    result.accept(JSON.object("status", "completed", "ar", JSON.object("position", coordinates)));
                });
                break;
            default:
                result.accept(Services.outcome("failed", "Unknown AR operation."));
        }
    }

    private static void check(GofastrActivity host, Consumer<JSONObject> result) {
        Handler main = new Handler(Looper.getMainLooper());
        AtomicBoolean done = new AtomicBoolean();
        Consumer<String> complete = availability -> {
            if (done.compareAndSet(false, true)) {
                result.accept(JSON.object("status", "completed", "ar", JSON.object("availability", availability)));
            }
        };
        Runnable timeout = () -> complete.accept("unknown");
        main.postDelayed(timeout, 5000);
        try {
            ArCoreApk.getInstance().checkAvailabilityAsync(host.getApplicationContext(), value -> main.post(() -> {
                main.removeCallbacks(timeout);
                complete.accept(availability(value));
            }));
        } catch (RuntimeException e) {
            main.removeCallbacks(timeout);
            complete.accept("unknown");
        }
    }

    private static String availability(ArCoreApk.Availability value) {
        switch (value) {
            case SUPPORTED_INSTALLED: return "ready";
            case SUPPORTED_NOT_INSTALLED: return "install-needed";
            case SUPPORTED_APK_TOO_OLD: return "update-needed";
            case UNSUPPORTED_DEVICE_NOT_CAPABLE: return "unsupported";
            default: return "unknown";
        }
    }
}
