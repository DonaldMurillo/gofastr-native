package dev.gofastr.nativeui.arcore;

import android.Manifest;
import android.app.Activity;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.res.Configuration;
import android.graphics.Color;
import android.os.Bundle;
import android.util.Log;
import android.view.Gravity;
import android.view.MotionEvent;
import android.view.View;
import android.widget.Button;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.TextView;
import android.opengl.GLSurfaceView;
import com.google.ar.core.ArCoreApk;
import com.google.ar.core.Config;
import com.google.ar.core.Session;
import com.google.ar.core.exceptions.UnavailableDeviceNotCompatibleException;
import com.google.ar.core.exceptions.UnavailableUserDeclinedInstallationException;

public final class ARSessionActivity extends Activity {
    private static final int CAMERA = 1;
    private static final String TAG = "gofastr-ar";
    private GLSurfaceView surface;
    private ARCameraRenderer renderer;
    private TextView instruction;
    private Button done;
    private volatile Session session;
    private volatile int rotation;
    private volatile boolean running;
    private boolean requestedInstall;
    private boolean requestedPermission;
    private float[] position;

    @Override protected void onCreate(Bundle state) {
        super.onCreate(state);
        if (state != null) {
            requestedInstall = state.getBoolean("requestedInstall");
            requestedPermission = state.getBoolean("requestedPermission");
        }
        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        surface = new GLSurfaceView(this);
        surface.setEGLContextClientVersion(2);
        renderer = new ARCameraRenderer(this);
        surface.setRenderer(renderer);
        surface.setOnTouchListener((view, event) -> {
            if (event.getAction() == MotionEvent.ACTION_UP) place(event.getX(), event.getY());
            return true;
        });
        surface.setImportantForAccessibility(View.IMPORTANT_FOR_ACCESSIBILITY_NO);
        root.addView(surface, new FrameLayout.LayoutParams(-1, -1));

        LinearLayout controls = new LinearLayout(this);
        controls.setOrientation(LinearLayout.VERTICAL);
        controls.setBackgroundColor(0xe6000000);
        int padding = Math.round(12 * getResources().getDisplayMetrics().density);
        controls.setPadding(padding, padding, padding, padding);
        instruction = new TextView(this);
        instruction.setTextColor(Color.WHITE);
        instruction.setTextSize(16);
        instruction.setAccessibilityLiveRegion(View.ACCESSIBILITY_LIVE_REGION_POLITE);
        instruction.setText("Starting Google ARCore…");
        controls.addView(instruction);
        LinearLayout buttons = new LinearLayout(this);
        buttons.setGravity(Gravity.END);
        Button cancel = button("Cancel", v -> settle("cancelled", null));
        Button center = button("Place at center", v -> place(surface.getWidth() * .5f, surface.getHeight() * .5f));
        done = button("Done", v -> settle("completed", null));
        done.setEnabled(false);
        buttons.addView(cancel);
        buttons.addView(center);
        buttons.addView(done);
        controls.addView(buttons);
        root.addView(controls, new FrameLayout.LayoutParams(-1, -2, Gravity.BOTTOM));
        setContentView(root);
    }

    private Button button(String label, View.OnClickListener action) {
        Button button = new Button(this);
        button.setText(label);
        button.setOnClickListener(action);
        return button;
    }

    @Override protected void onSaveInstanceState(Bundle state) {
        super.onSaveInstanceState(state);
        state.putBoolean("requestedInstall", requestedInstall);
        state.putBoolean("requestedPermission", requestedPermission);
    }

    @Override protected void onResume() {
        super.onResume();
        updateRotation();
        startSession();
    }

    private void startSession() {
        if (isFinishing() || running) return;
        try {
            if (session == null) {
                ArCoreApk core = ArCoreApk.getInstance();
                if (core.checkAvailability(this).isUnsupported()) {
                    settle("unsupported", "This device does not support Google ARCore.");
                    return;
                }
                if (core.requestInstall(this, !requestedInstall) == ArCoreApk.InstallStatus.INSTALL_REQUESTED) {
                    requestedInstall = true;
                    return;
                }
                if (checkSelfPermission(Manifest.permission.CAMERA) != PackageManager.PERMISSION_GRANTED) {
                    if (!requestedPermission) {
                        requestedPermission = true;
                        requestPermissions(new String[]{Manifest.permission.CAMERA}, CAMERA);
                    }
                    return;
                }
                session = new Session(this);
                Config config = new Config(session);
                config.setPlaneFindingMode(Config.PlaneFindingMode.HORIZONTAL_AND_VERTICAL);
                session.configure(config);
            }
            session.resume();
            running = true;
            surface.onResume();
        } catch (UnavailableDeviceNotCompatibleException e) {
            settle("unsupported", "This device does not support Google ARCore.");
        } catch (UnavailableUserDeclinedInstallationException e) {
            settle("cancelled", "Google Play Services for AR was not installed or updated.");
        } catch (SecurityException e) {
            settle("denied", "Camera access is required for AR.");
        } catch (Exception e) {
            fail(e);
        }
    }

    @Override public void onRequestPermissionsResult(int code, String[] permissions, int[] grants) {
        super.onRequestPermissionsResult(code, permissions, grants);
        if (code == CAMERA && (grants.length == 0 || grants[0] != PackageManager.PERMISSION_GRANTED)) {
            settle("denied", "Camera access is required for AR.");
        }
    }

    @Override public void onConfigurationChanged(Configuration configuration) {
        super.onConfigurationChanged(configuration);
        updateRotation();
    }

    private void updateRotation() {
        rotation = getWindowManager().getDefaultDisplay().getRotation();
    }

    @Override protected void onPause() {
        if (running) {
            surface.onPause();
            session.pause();
            running = false;
        }
        super.onPause();
    }

    @Override protected void onDestroy() {
        if (session != null) {
            session.close();
            session = null;
        }
        super.onDestroy();
    }

    private void place(float x, float y) {
        if (running) surface.queueEvent(() -> renderer.place(x, y));
    }

    Session session() { return running ? session : null; }
    int rotation() { return rotation; }

    void updatePlacement(String text, float[] anchorPosition) {
        runOnUiThread(() -> {
            if (!running || isFinishing() || isDestroyed()) return;
            position = anchorPosition;
            instruction.setText(text);
            done.setEnabled(position != null);
        });
    }

    void fail(Exception e) {
        Log.e(TAG, "AR session failed", e);
        runOnUiThread(() -> settle("failed", "Could not start or continue the AR session."));
    }

    private void settle(String status, String message) {
        if (isFinishing() || isDestroyed()) return;
        Intent data = new Intent().putExtra("status", status);
        if (message != null) data.putExtra("message", message);
        if ("completed".equals(status)) data.putExtra("position", position);
        setResult(RESULT_OK, data);
        finish();
    }
}
