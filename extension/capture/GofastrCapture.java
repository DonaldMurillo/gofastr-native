package dev.gofastr.nativeui;

import android.Manifest;
import android.content.Context;
import android.graphics.Color;
import android.graphics.Matrix;
import android.graphics.RectF;
import android.graphics.SurfaceTexture;
import android.os.Build;
import android.os.SystemClock;
import android.util.Size;
import android.view.Gravity;
import android.view.Surface;
import android.view.TextureView;
import android.view.View;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.TextView;
import org.json.JSONObject;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.function.Consumer;

/** The capture library's compiled component: camera preview and session start. */
public final class GofastrCapture implements Extensions.ComponentFactory {
    /** Sessions this process already tried to start, so a remount never restarts one. */
    private static final Set<String> requested = Collections.synchronizedSet(new HashSet<>());

    @Override public Map<String, String> capabilities(GofastrActivity host) {
        GofastrCaptureEngine engine = GofastrCaptureEngine.get(host);
        engine.attachStorage();
        return Collections.singletonMap("gofastr.capture/v1", engine.hasCamera() ? "available" : "unavailable");
    }

    @Override public Extensions.Component mount(GofastrActivity host, JSONObject payload, Consumer<String> emit) throws Exception {
        Preview preview = new Preview(host);
        preview.update(payload, emit);
        return preview;
    }

    private static final class Preview implements Extensions.Component, TextureView.SurfaceTextureListener {
        private final GofastrActivity host;
        private final GofastrCaptureEngine engine;
        private final Frame frame;
        private final TextureView texture;
        private final TextView status;
        private final Runnable observer = this::refreshSoon;
        private final Runnable tick = this::refresh;
        private SurfaceTexture surface;
        private boolean previewWanted, visible = true, suspended, attached, disposed, askedCamera;
        private String facing = "back";

        Preview(GofastrActivity host) {
            this.host = host;
            this.engine = GofastrCaptureEngine.get(host);
            frame = new Frame(host);
            frame.setBackgroundColor(Color.BLACK);
            texture = new TextureView(host);
            texture.setSurfaceTextureListener(this);
            texture.setContentDescription("Camera preview");
            frame.addView(texture, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
            status = new TextView(host);
            status.setTextColor(Color.WHITE);
            status.setBackgroundColor(0x99000000);
            int pad = Math.round(8 * host.getResources().getDisplayMetrics().density);
            status.setPadding(pad, pad / 2, pad, pad / 2);
            FrameLayout.LayoutParams statusLayout = new FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.BOTTOM | Gravity.START);
            frame.addView(status, statusLayout);
            GofastrActivity.identify(frame, "native.component.capture");
            engine.observe(observer);
        }

        @Override public View view() { return frame; }

        @Override public void update(JSONObject payload, Consumer<String> emit) {
            suspended = false;
            previewWanted = payload.optBoolean("preview");
            facing = payload.optString("camera", "back");
            JSONObject session = payload.optJSONObject("session");
            if (session != null) {
                String id = session.optString("id");
                JSONObject config = session.optJSONObject("config");
                if (config != null && !id.equals(engine.sessionId) && requested.add(id)) begin(id, config);
            } else if (previewWanted && !askedCamera && !engine.granted(Manifest.permission.CAMERA)) {
                askedCamera = true;
                host.requestExtensionPermissions(new String[] {Manifest.permission.CAMERA}, grants -> refresh());
            }
            attach();
            refresh();
        }

        /** Asks for what the session needs, then hands it to the foreground service. */
        private void begin(String id, JSONObject config) {
            List<String> permissions = new ArrayList<>();
            permissions.add(Manifest.permission.CAMERA);
            if (config.optBoolean("location")) {
                permissions.add(Manifest.permission.ACCESS_FINE_LOCATION);
                permissions.add(Manifest.permission.ACCESS_COARSE_LOCATION);
            }
            if (config.optBoolean("audio") && GofastrCaptureEngine.declared(host, Manifest.permission.RECORD_AUDIO)) {
                permissions.add(Manifest.permission.RECORD_AUDIO);
            }
            if (Build.VERSION.SDK_INT >= 33) permissions.add("android.permission.POST_NOTIFICATIONS");
            host.requestExtensionPermissions(permissions.toArray(new String[0]), grants -> {
                if (!Boolean.TRUE.equals(grants.get(Manifest.permission.CAMERA))) {
                    engine.attachStorage();
                    try {
                        capturebind.Capturebind.event(id, "failed", "{\"reason\":\"denied\",\"message\":\"Camera permission was denied.\"}");
                    } catch (Exception ignored) {}
                    return;
                }
                try {
                    // The answer may reach an activity that replaced this one.
                    GofastrCaptureService.start(host.getApplicationContext(), id, config);
                } catch (RuntimeException e) {
                    try {
                        capturebind.Capturebind.event(id, "failed", "{\"reason\":\"service\",\"message\":" + JSONObject.quote(String.valueOf(e.getMessage())) + "}");
                    } catch (Exception ignored) {}
                }
                refresh();
            });
        }

        private void attach() {
            boolean want = !disposed && visible && !suspended && surface != null && (previewWanted || engine.sessionId != null) && engine.granted(Manifest.permission.CAMERA);
            if (want && !attached) {
                attached = true;
                engine.attachPreview(surface, facing);
            } else if (!want && attached) {
                attached = false;
                engine.detachPreview(surface, false);
            }
            transform();
        }

        /** The host left this screen or the app paused. Preview-only cameras close; sessions keep running. */
        @Override public void suspend() {
            suspended = true;
            attach();
        }

        /** The host does not report resuming, so the window does. */
        private void resumed() {
            if (!suspended || disposed) return;
            suspended = false;
            refresh();
        }

        @Override public void dispose() {
            disposed = true;
            attach();
            engine.unobserve(observer);
            host.main.removeCallbacks(tick);
        }

        @Override public void onSurfaceTextureAvailable(SurfaceTexture texture, int width, int height) {
            surface = texture;
            attach();
        }

        @Override public void onSurfaceTextureSizeChanged(SurfaceTexture texture, int width, int height) { transform(); }

        /**
         * Android destroys the texture whenever the window stops. The camera
         * may still be configuring with it, so the engine releases it on its
         * own thread, after any queued work that still uses it.
         */
        @Override public boolean onSurfaceTextureDestroyed(SurfaceTexture texture) {
            engine.detachPreview(texture, true);
            attached = false;
            surface = null;
            return false;
        }

        @Override public void onSurfaceTextureUpdated(SurfaceTexture texture) {}

        private void refreshSoon() { host.main.post(this::refresh); }

        private void refresh() {
            host.main.removeCallbacks(tick);
            if (disposed) return;
            String state = engine.status;
            if (engine.sessionId != null && !state.isEmpty()) {
                long seconds = (SystemClock.elapsedRealtime() - engine.startedAt) / 1000;
                status.setText(engine.startedAt == 0 ? "● " + state : String.format(java.util.Locale.US, "● %s %02d:%02d", state, seconds / 60, seconds % 60));
                status.setVisibility(View.VISIBLE);
                host.main.postDelayed(tick, 1000);
            } else if (!engine.granted(Manifest.permission.CAMERA) && (previewWanted || engine.sessionId != null)) {
                status.setText("Camera permission needed");
                status.setVisibility(View.VISIBLE);
            } else {
                status.setVisibility(View.GONE);
            }
            frame.requestLayout();
            attach();
        }

        /** Rotates and scales the camera buffer for the current display rotation. */
        private void transform() {
            Size size = engine.size;
            int width = texture.getWidth(), height = texture.getHeight();
            if (size == null || width == 0 || height == 0) return;
            int rotation = engine.displayRotation();
            Matrix matrix = new Matrix();
            RectF view = new RectF(0, 0, width, height);
            RectF buffer = new RectF(0, 0, size.getHeight(), size.getWidth());
            float cx = view.centerX(), cy = view.centerY();
            if (rotation == Surface.ROTATION_90 || rotation == Surface.ROTATION_270) {
                buffer.offset(cx - buffer.centerX(), cy - buffer.centerY());
                matrix.setRectToRect(view, buffer, Matrix.ScaleToFit.FILL);
                float scale = Math.max((float) height / size.getHeight(), (float) width / size.getWidth());
                matrix.postScale(scale, scale, cx, cy);
                matrix.postRotate(90 * (rotation - 2), cx, cy);
            } else if (rotation == Surface.ROTATION_180) {
                matrix.postRotate(180, cx, cy);
            }
            texture.setTransform(matrix);
        }

        /** Sizes itself to the camera's aspect ratio and tracks visibility. */
        private final class Frame extends FrameLayout {
            Frame(Context context) { super(context); }

            @Override protected void onMeasure(int widthSpec, int heightSpec) {
                int width = MeasureSpec.getSize(widthSpec);
                Size size = engine.size;
                double ratio = 9.0 / 16.0;
                if (size != null) {
                    int display = engine.displayRotation() * 90;
                    boolean swapped = ((engine.sensorOrientation - display) % 180 + 180) % 180 != 0;
                    ratio = swapped ? (double) size.getWidth() / size.getHeight() : (double) size.getHeight() / size.getWidth();
                }
                int height = (int) Math.round(width * ratio);
                int maximum = (int) (getResources().getDisplayMetrics().heightPixels * 0.6);
                int textureWidth = width;
                if (height > maximum) {
                    // Keep the picture's aspect ratio: narrow it rather than stretch it.
                    height = maximum;
                    textureWidth = (int) Math.round(height / ratio);
                }
                FrameLayout.LayoutParams layout = (FrameLayout.LayoutParams) texture.getLayoutParams();
                if (layout.width != textureWidth || layout.gravity != Gravity.CENTER) {
                    layout.width = textureWidth;
                    layout.gravity = Gravity.CENTER;
                    texture.setLayoutParams(layout);
                }
                super.onMeasure(MeasureSpec.makeMeasureSpec(width, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(height, MeasureSpec.EXACTLY));
            }

            @Override public void onWindowFocusChanged(boolean focused) {
                super.onWindowFocusChanged(focused);
                if (focused) resumed();
            }

            @Override protected void onWindowVisibilityChanged(int visibility) {
                super.onWindowVisibilityChanged(visibility);
                if (visibility == View.VISIBLE && hasWindowFocus()) resumed();
            }

            @Override public void onVisibilityAggregated(boolean isVisible) {
                super.onVisibilityAggregated(isVisible);
                visible = isVisible;
                attach();
            }
        }
    }
}
