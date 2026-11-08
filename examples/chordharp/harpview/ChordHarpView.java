package dev.gofastr.nativeui;

import android.app.Activity;
import android.app.Application;
import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Paint;
import android.graphics.RectF;
import android.graphics.Typeface;
import android.media.AudioAttributes;
import android.media.AudioFormat;
import android.media.AudioManager;
import android.media.AudioTrack;
import android.os.Build;
import android.os.Bundle;
import android.os.Process;
import android.os.SystemClock;
import android.util.SparseArray;
import android.view.HapticFeedbackConstants;
import android.view.MotionEvent;
import android.view.View;
import android.view.ViewParent;
import android.widget.ScrollView;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.ArrayList;
import java.util.List;

import chordharp.Chordharp;

/**
 * The chord harp: a chord grid, a strumplate and a rhythm bar on one canvas.
 * Touches call the bound Go package directly and an audio thread pulls
 * samples from it, so nothing waits on a screen render.
 *
 * The grid has a row per chord kind and a column per root (landscape), or
 * the transpose (portrait). Chord pads are momentary: the chord sounds while
 * a finger holds it, and sliding to another pad changes chord. With Latch
 * on, a tap holds the chord until the pad is tapped again. The strumplate's
 * twelve zones run low at the bottom to high at the top; dragging across
 * it plucks every zone passed.
 */
final class ChordHarpView extends View {
    private static final int BODY = 0xff16151a, PANEL = 0xff221f27, LINE = 0xff3a3542, INK = 0xfff3efe6, MUTED = 0xffa59fae;
    private static final int[] ROW = {0xffffc857, 0xff5bc0eb, 0xffe36bae}; // major, minor, seventh
    private static final int ZONES = 12;

    private final GofastrActivity host;
    private final float density;
    private final Paint fill = new Paint(Paint.ANTI_ALIAS_FLAG), text = new Paint(Paint.ANTI_ALIAS_FLAG);

    private String[] kinds = new String[0], patterns = new String[0];
    private String[][] chords = new String[0][];
    private JSONObject state = new JSONObject();

    // Layout, rebuilt on every size change.
    private final RectF grid = new RectF(), plate = new RectF();
    private boolean portrait;
    private final List<Control> controls = new ArrayList<>();
    private RectF beatsRect, chordRect, patternRect, tempoRect;

    // Pointers: which pad, zone or control each finger is on.
    private final SparseArray<int[]> padPointers = new SparseArray<>();
    private final SparseArray<Integer> zonePointers = new SparseArray<>();
    private final SparseArray<Control> controlPointers = new SparseArray<>();
    private final List<Integer> padOrder = new ArrayList<>();
    private final long[] glow = new long[ZONES];

    private Audio audio;

    // The host suspends the component when the activity pauses, but a
    // pause and resume can happen without the window losing focus or
    // visibility (a relaunch intent, a dialog over the app). Resuming
    // starts the sound again.
    private final Application.ActivityLifecycleCallbacks resumed = new Application.ActivityLifecycleCallbacks() {
        @Override public void onActivityResumed(Activity activity) {
            if (activity == host && isAttachedToWindow() && getWindowVisibility() == VISIBLE) startAudio();
        }
        @Override public void onActivityCreated(Activity activity, Bundle state) {}
        @Override public void onActivityStarted(Activity activity) {}
        @Override public void onActivityPaused(Activity activity) {}
        @Override public void onActivityStopped(Activity activity) {}
        @Override public void onActivitySaveInstanceState(Activity activity, Bundle state) {}
        @Override public void onActivityDestroyed(Activity activity) {}
    };

    ChordHarpView(GofastrActivity host, JSONObject payload) {
        super(host);
        this.host = host;
        density = getResources().getDisplayMetrics().density;
        text.setTypeface(Typeface.create(Typeface.DEFAULT, Typeface.BOLD));
        text.setTextAlign(Paint.Align.CENTER);
        setHapticFeedbackEnabled(true);
        GofastrActivity.identify(this, "chordharp.instrument");
        layout(payload);
    }

    /** Reads the grid labels and pattern names Go sent. */
    void layout(JSONObject payload) {
        JSONArray k = payload.optJSONArray("kinds"), c = payload.optJSONArray("chords"), p = payload.optJSONArray("patterns");
        kinds = strings(k);
        patterns = strings(p);
        chords = new String[c == null ? 0 : c.length()][];
        for (int i = 0; i < chords.length; i++) chords[i] = strings(c.optJSONArray(i));
        refresh();
    }

    private static String[] strings(JSONArray a) {
        String[] out = new String[a == null ? 0 : a.length()];
        for (int i = 0; i < out.length; i++) out[i] = a.optString(i);
        return out;
    }

    private void refresh() {
        try { state = new JSONObject(Chordharp.state()); } catch (Exception e) { state = new JSONObject(); }
        JSONArray zones = state.optJSONArray("zones");
        String chord = state.optString("chord");
        setContentDescription("Chord harp. " + (state.optBoolean("held") ? chord + " held, " + state.optString("tones") : "No chord held")
            + ". Rhythm " + (state.optBoolean("playing") ? "playing" : "stopped") + ", " + pattern() + " at " + state.optInt("tempo") + " beats per minute."
            + (zones != null ? " Strumplate from " + zones.optString(0) + " to " + zones.optString(zones.length() - 1) + "." : ""));
        invalidate();
    }

    private String pattern() {
        int i = state.optInt("pattern");
        return i >= 0 && i < patterns.length ? patterns[i] : "";
    }

    private float dp(float v) { return v * density; }

    // ---- size ----------------------------------------------------------

    /** Fills the screen below whatever the host put above the instrument. */
    @Override protected void onMeasure(int widthSpec, int heightSpec) {
        int width = MeasureSpec.getSize(widthSpec);
        int height = Math.round(dp(360));
        ScrollView scroll = scroller();
        if (scroll != null && scroll.getHeight() > 0) {
            int[] mine = new int[2], theirs = new int[2];
            getLocationInWindow(mine);
            scroll.getLocationInWindow(theirs);
            int top = mine[1] - theirs[1] + scroll.getScrollY();
            // The host's content column has 28dp of padding below us.
            height = Math.max(Math.round(dp(200)), scroll.getHeight() - top - Math.round(dp(28)));
        }
        setMeasuredDimension(width, height);
    }

    private ScrollView scroller() {
        for (ViewParent p = getParent(); p != null; p = p.getParent()) if (p instanceof ScrollView) return (ScrollView) p;
        return null;
    }

    // The first measure runs before the scroller has a height, and a
    // rotation changes it: measure again after the scroller's layout.
    private final OnLayoutChangeListener resize = (v, l, t, r, b, ol, ot, or, ob) -> { if (b - t != ob - ot) post(this::requestLayout); };

    @Override protected void onSizeChanged(int w, int h, int ow, int oh) {
        float pad = dp(10), gap = dp(10);
        portrait = h > w;
        controls.clear();
        float barTop = pad, row = dp(48);
        boolean twoRows = w < dp(600);
        float barBottom = barTop + row * (twoRows ? 2 : 1) + (twoRows ? dp(6) : 0);
        // Row one: transport and pattern.
        float x = pad, y = barTop;
        controls.add(new Control("toggle", new RectF(x, y, x + dp(92), y + row)));
        x += dp(92) + gap;
        controls.add(new Control("pattern-", new RectF(x, y, x + dp(48), y + row)));
        x += dp(48);
        float patternWidth = twoRows ? w - pad - dp(48) - x - gap - dp(64) : dp(124);
        patternRect = new RectF(x, y, x + patternWidth, y + row);
        x += patternWidth;
        controls.add(new Control("pattern+", new RectF(x, y, x + dp(48), y + row)));
        x += dp(48) + gap;
        if (twoRows) {
            beatsRect = new RectF(x, y, w - pad, y + row);
            x = pad;
            y += row + dp(6);
        }
        // Tempo, latch, and (one row) the beat lights.
        controls.add(new Control("tempo-", new RectF(x, y, x + dp(48), y + row)));
        x += dp(48);
        tempoRect = new RectF(x, y, x + dp(76), y + row);
        x += dp(76);
        controls.add(new Control("tempo+", new RectF(x, y, x + dp(48), y + row)));
        x += dp(48) + gap;
        controls.add(new Control("latch", new RectF(x, y, x + dp(84), y + row)));
        x += dp(84) + gap;
        if (!twoRows) {
            beatsRect = new RectF(x, y, x + dp(76), y + row);
            x += dp(76) + gap;
        }
        chordRect = new RectF(x, y, w - pad, y + row);

        float top = barBottom + gap, bottom = h - pad;
        float plateWidth = Math.max(dp(110), (w - 2 * pad) * (portrait ? 0.32f : 0.3f));
        plate.set(w - pad - plateWidth, top, w - pad, bottom);
        grid.set(pad, top, plate.left - gap, bottom);
    }

    // ---- drawing -------------------------------------------------------

    @Override protected void onDraw(Canvas canvas) {
        long now = SystemClock.uptimeMillis();
        fill.setColor(BODY);
        canvas.drawRoundRect(0, 0, getWidth(), getHeight(), dp(16), dp(16), fill);
        drawBar(canvas);
        drawGrid(canvas);
        boolean glowing = drawPlate(canvas, now);
        if (glowing || state.optBoolean("playing")) postInvalidateOnAnimation();
    }

    private void drawBar(Canvas canvas) {
        boolean playing = state.optBoolean("playing"), latch = state.optBoolean("latch");
        for (Control c : controls) {
            boolean pressed = controlPointers.indexOfValue(c) >= 0;
            boolean on = c.id.equals("toggle") && playing || c.id.equals("latch") && latch;
            fill.setColor(on ? 0xfff0a64a : pressed ? LINE : PANEL);
            canvas.drawRoundRect(inset(c.rect, dp(3)), dp(10), dp(10), fill);
            String label;
            switch (c.id) {
                case "toggle": label = playing ? "■ Stop" : "▶ Start"; break;
                case "pattern-": label = "‹"; break;
                case "pattern+": label = "›"; break;
                case "tempo-": label = "−"; break;
                case "tempo+": label = "+"; break;
                default: label = "Latch";
            }
            label(canvas, label, c.rect, on ? BODY : INK, c.id.endsWith("-") || c.id.endsWith("+") ? 22 : 15);
        }
        label(canvas, pattern(), patternRect, INK, 15);
        label(canvas, state.optInt("tempo") + " bpm", tempoRect, INK, 15);
        drawBeats(canvas);
        text.setTextAlign(Paint.Align.RIGHT);
        label(canvas, chordReadout(), chordRect, state.optBoolean("held") ? INK : MUTED, 15);
        text.setTextAlign(Paint.Align.CENTER);
    }

    private void drawBeats(Canvas canvas) {
        int steps = Math.max(state.optInt("steps"), 1), perBeat = Math.max(state.optInt("perBeat"), 1), beats = steps / perBeat;
        int step = (int) Chordharp.step(), beat = step < 0 ? -1 : step / perBeat;
        float r = dp(5), spacing = Math.min(dp(16), beatsRect.width() / Math.max(beats, 1));
        float x = beatsRect.centerX() - spacing * (beats - 1) / 2f, y = beatsRect.centerY();
        for (int i = 0; i < beats; i++) {
            fill.setColor(i == beat ? (i == 0 ? 0xfff0a64a : INK) : LINE);
            canvas.drawCircle(x + i * spacing, y, i == beat ? r * 1.3f : r, fill);
        }
    }

    private void drawGrid(Canvas canvas) {
        int rows = kinds.length, cols = chords.length > 0 ? chords[0].length : 0;
        if (rows == 0 || cols == 0) return;
        boolean held = state.optBoolean("held");
        int heldKind = state.optInt("kind"), heldCol = state.optInt("column");
        for (int k = 0; k < rows; k++) {
            for (int c = 0; c < cols; c++) {
                RectF r = inset(cell(k, c), dp(3));
                boolean on = held && k == heldKind && c == heldCol;
                int color = ROW[k % ROW.length];
                fill.setColor(on ? color : blend(color, PANEL, 0.18f));
                canvas.drawRoundRect(r, dp(10), dp(10), fill);
                if (!on) {
                    fill.setColor(blend(color, PANEL, 0.55f));
                    canvas.drawRect(r.left + dp(10), r.bottom - dp(5), r.right - dp(10), r.bottom - dp(3), fill);
                }
                label(canvas, chords[k][c], r, on ? BODY : color, Math.min(20, r.height() / density * 0.42f));
            }
        }
    }

    /** The pad for chord kind k and grid column c. */
    private RectF cell(int k, int c) {
        int rows = kinds.length, cols = chords[0].length;
        if (portrait) {
            float w = grid.width() / rows, h = grid.height() / cols;
            return new RectF(grid.left + k * w, grid.top + c * h, grid.left + (k + 1) * w, grid.top + (c + 1) * h);
        }
        float w = grid.width() / cols, h = grid.height() / rows;
        return new RectF(grid.left + c * w, grid.top + k * h, grid.left + (c + 1) * w, grid.top + (k + 1) * h);
    }

    private boolean drawPlate(Canvas canvas, long now) {
        fill.setColor(PANEL);
        canvas.drawRoundRect(plate, dp(14), dp(14), fill);
        int color = ROW[state.optInt("kind") % ROW.length];
        JSONArray zones = state.optJSONArray("zones");
        float h = plate.height() / ZONES;
        boolean glowing = false;
        for (int z = 0; z < ZONES; z++) {
            float bottom = plate.bottom - z * h, top = bottom - h;
            float age = (now - glow[z]) / 600f;
            boolean lit = glow[z] > 0 && age < 1;
            if (lit) {
                glowing = true;
                fill.setColor(blend(color, PANEL, 0.85f * (1 - age)));
                canvas.drawRoundRect(plate.left + dp(4), top + dp(2), plate.right - dp(4), bottom - dp(2), dp(8), dp(8), fill);
            }
            if (z > 0) {
                fill.setColor(z % 3 == 0 ? blend(color, LINE, 0.5f) : LINE);
                canvas.drawRect(plate.left + dp(12), bottom - dp(1), plate.right - dp(12), bottom, fill);
            }
            if (zones != null) {
                text.setTextAlign(Paint.Align.LEFT);
                text.setTextSize(dp(12));
                text.setColor(lit && age < 0.6f ? INK : MUTED);
                canvas.drawText(zones.optString(z), plate.left + dp(12), top + h / 2 + dp(4), text);
                text.setTextAlign(Paint.Align.CENTER);
            }
        }
        text.setTextSize(dp(11));
        text.setColor(MUTED);
        canvas.drawText("strum ↕", plate.centerX() + dp(14), plate.top + dp(14), text);
        return glowing;
    }

    private void label(Canvas canvas, String s, RectF r, int color, float sp) {
        text.setColor(color);
        text.setTextSize(sp * density);
        float x = text.getTextAlign() == Paint.Align.RIGHT ? r.right - dp(4) : r.centerX();
        canvas.drawText(s, x, r.centerY() - (text.descent() + text.ascent()) / 2, text);
    }

    /** The held chord, as long as the bar has room for: "C major · C E G" down to "C". */
    private String chordReadout() {
        String[] choices = state.optBoolean("held")
            ? new String[]{state.optString("chord") + "  ·  " + state.optString("tones"), state.optString("short") + "  ·  " + state.optString("tones"), state.optString("chord"), state.optString("short")}
            : new String[]{"Hold a chord, then strum", "Hold a chord", "No chord"};
        text.setTextSize(15 * density);
        for (String s : choices) if (text.measureText(s) <= chordRect.width() - dp(4)) return s;
        return ellipsize(choices[choices.length - 1], chordRect.width() - dp(4), 15);
    }

    private String ellipsize(String s, float width, float sp) {
        text.setTextSize(sp * density);
        if (text.measureText(s) <= width) return s;
        while (s.length() > 1 && text.measureText(s + "…") > width) s = s.substring(0, s.length() - 1);
        return s + "…";
    }

    private static RectF inset(RectF r, float by) { return new RectF(r.left + by, r.top + by, r.right - by, r.bottom - by); }

    private static int blend(int color, int base, float amount) {
        int r = (int) (((base >> 16) & 0xff) + ((((color >> 16) & 0xff) - ((base >> 16) & 0xff)) * amount));
        int g = (int) (((base >> 8) & 0xff) + ((((color >> 8) & 0xff) - ((base >> 8) & 0xff)) * amount));
        int b = (int) ((base & 0xff) + (((color & 0xff) - (base & 0xff)) * amount));
        return 0xff000000 | (r << 16) | (g << 8) | b;
    }

    // ---- touch ---------------------------------------------------------

    @Override public boolean onTouchEvent(MotionEvent e) {
        int action = e.getActionMasked(), index = e.getActionIndex();
        switch (action) {
            case MotionEvent.ACTION_DOWN:
            case MotionEvent.ACTION_POINTER_DOWN:
                getParent().requestDisallowInterceptTouchEvent(true);
                down(e.getPointerId(index), e.getX(index), e.getY(index));
                break;
            case MotionEvent.ACTION_MOVE:
                for (int i = 0; i < e.getPointerCount(); i++) {
                    int id = e.getPointerId(i);
                    for (int h = 0; h < e.getHistorySize(); h++) move(id, e.getHistoricalX(i, h), e.getHistoricalY(i, h));
                    move(id, e.getX(i), e.getY(i));
                }
                break;
            case MotionEvent.ACTION_UP:
            case MotionEvent.ACTION_POINTER_UP:
                up(e.getPointerId(index), e.getX(index), e.getY(index), false);
                break;
            case MotionEvent.ACTION_CANCEL:
                for (int i = 0; i < e.getPointerCount(); i++) up(e.getPointerId(i), -1, -1, true);
                break;
            default:
                return false;
        }
        return true;
    }

    private void down(int id, float x, float y) {
        int[] pad = padAt(x, y);
        if (pad != null) {
            boolean latch = state.optBoolean("latch");
            if (latch && state.optBoolean("held") && state.optInt("kind") == pad[0] && state.optInt("column") == pad[1]) Chordharp.release();
            else Chordharp.hold(pad[0], pad[1]);
            padPointers.put(id, pad);
            padOrder.add(id);
            performHapticFeedback(HapticFeedbackConstants.VIRTUAL_KEY);
            refresh();
            return;
        }
        if (plate.contains(x, y)) {
            int z = zoneAt(y);
            zonePointers.put(id, z);
            pluck(z, 0);
            invalidate();
            return;
        }
        for (Control c : controls) {
            if (c.rect.contains(x, y)) {
                controlPointers.put(id, c);
                if (c.repeats()) { press(c); c.scheduleRepeat(); }
                invalidate();
                return;
            }
        }
    }

    private void move(int id, float x, float y) {
        Integer from = zonePointers.get(id);
        if (from != null) {
            int to = zoneAt(y);
            if (to != from) {
                // Pluck every zone passed, spaced a few milliseconds apart,
                // so a fast swipe still sounds as a strum.
                int dir = to > from ? 1 : -1, n = 0;
                for (int z = from + dir; z != to + dir; z += dir) pluck(z, 0.004 * n++);
                zonePointers.put(id, to);
                invalidate();
            }
            return;
        }
        int[] pad = padPointers.get(id);
        if (pad != null && !state.optBoolean("latch")) {
            int[] now = padAt(x, y);
            if (now != null && (now[0] != pad[0] || now[1] != pad[1])) {
                padPointers.put(id, now);
                Chordharp.hold(now[0], now[1]);
                performHapticFeedback(HapticFeedbackConstants.VIRTUAL_KEY);
                refresh();
            }
        }
    }

    private void up(int id, float x, float y, boolean cancel) {
        zonePointers.remove(id);
        Control c = controlPointers.get(id);
        if (c != null) {
            controlPointers.remove(id);
            c.cancelRepeat();
            if (!cancel && !c.repeats() && c.rect.contains(x, y)) press(c);
            invalidate();
        }
        if (padPointers.get(id) != null) {
            padPointers.remove(id);
            padOrder.remove(Integer.valueOf(id));
            if (!state.optBoolean("latch")) {
                // Another finger still on a pad takes over; otherwise let go.
                if (padOrder.isEmpty()) Chordharp.release();
                else { int[] last = padPointers.get(padOrder.get(padOrder.size() - 1)); Chordharp.hold(last[0], last[1]); }
            }
            refresh();
        }
    }

    private void press(Control c) {
        switch (c.id) {
            case "toggle": Chordharp.toggle(); break;
            case "pattern-": Chordharp.nextPattern(-1); break;
            case "pattern+": Chordharp.nextPattern(1); break;
            case "tempo-": Chordharp.nudgeTempo(-2); break;
            case "tempo+": Chordharp.nudgeTempo(2); break;
            case "latch":
                boolean on = !state.optBoolean("latch");
                Chordharp.setLatch(on);
                if (!on && padOrder.isEmpty()) Chordharp.release();
                break;
        }
        performHapticFeedback(HapticFeedbackConstants.VIRTUAL_KEY);
        refresh();
    }

    private void pluck(int zone, double delay) {
        Chordharp.pluck(zone, delay);
        glow[zone] = SystemClock.uptimeMillis() + Math.round(delay * 1000);
    }

    private int[] padAt(float x, float y) {
        if (!grid.contains(x, y) || chords.length == 0) return null;
        int rows = kinds.length, cols = chords[0].length;
        int a = (int) ((x - grid.left) / grid.width() * (portrait ? rows : cols));
        int b = (int) ((y - grid.top) / grid.height() * (portrait ? cols : rows));
        int k = portrait ? a : b, c = portrait ? b : a;
        return new int[]{Math.min(Math.max(k, 0), rows - 1), Math.min(Math.max(c, 0), cols - 1)};
    }

    private int zoneAt(float y) {
        int z = (int) ((plate.bottom - y) / plate.height() * ZONES);
        return Math.min(Math.max(z, 0), ZONES - 1);
    }

    /** A bar button. Tempo buttons repeat while held. */
    private final class Control {
        final String id;
        final RectF rect;
        final Runnable repeat = new Runnable() { @Override public void run() { press(Control.this); postDelayed(this, 90); } };
        Control(String id, RectF rect) { this.id = id; this.rect = rect; }
        boolean repeats() { return id.startsWith("tempo"); }
        void scheduleRepeat() { postDelayed(repeat, 450); }
        void cancelRepeat() { removeCallbacks(repeat); }
    }

    // ---- audio ---------------------------------------------------------

    @Override protected void onAttachedToWindow() {
        super.onAttachedToWindow();
        ScrollView scroll = scroller();
        if (scroll != null) scroll.addOnLayoutChangeListener(resize);
        host.getApplication().registerActivityLifecycleCallbacks(resumed);
        if (getWindowVisibility() == VISIBLE) startAudio();
    }

    @Override protected void onDetachedFromWindow() {
        ScrollView scroll = scroller();
        if (scroll != null) scroll.removeOnLayoutChangeListener(resize);
        host.getApplication().unregisterActivityLifecycleCallbacks(resumed);
        pauseAudio();
        super.onDetachedFromWindow();
    }

    @Override protected void onWindowVisibilityChanged(int visibility) {
        super.onWindowVisibilityChanged(visibility);
        if (visibility == VISIBLE) startAudio(); else pauseAudio();
    }

    // Brings the sound back when the window regains focus.
    @Override public void onWindowFocusChanged(boolean focused) {
        super.onWindowFocusChanged(focused);
        if (focused && isAttachedToWindow() && getWindowVisibility() == VISIBLE) startAudio();
    }

    void startAudio() {
        if (audio != null) return;
        try {
            audio = new Audio(getContext());
            audio.start();
        } catch (Exception e) {
            audio = null;
            android.util.Log.w("gofastr-native", "chord harp audio did not start", e);
        }
    }

    void pauseAudio() {
        if (audio == null) return;
        audio.finish();
        audio = null;
        for (int i = 0; i < controlPointers.size(); i++) controlPointers.valueAt(i).cancelRepeat();
    }

    /**
     * Pulls 16-bit mono PCM from Go and writes it to a low-latency track.
     * If the track fails (an audio route change can kill it), the thread
     * logs it and builds a new one, so the instrument never goes quiet.
     */
    private static final class Audio extends Thread {
        private final int rate, frames;
        private volatile boolean running = true;

        Audio(Context context) {
            super("chordharp-audio");
            AudioManager manager = (AudioManager) context.getSystemService(Context.AUDIO_SERVICE);
            rate = AudioTrack.getNativeOutputSampleRate(AudioManager.STREAM_MUSIC);
            int burst = 256;
            try { burst = Integer.parseInt(manager.getProperty(AudioManager.PROPERTY_OUTPUT_FRAMES_PER_BUFFER)); } catch (Exception ignored) {}
            frames = Math.min(Math.max(burst, 128), 1024);
            Chordharp.open(rate);
        }

        private AudioTrack track() {
            int min = AudioTrack.getMinBufferSize(rate, AudioFormat.CHANNEL_OUT_MONO, AudioFormat.ENCODING_PCM_16BIT);
            AudioTrack.Builder builder = new AudioTrack.Builder()
                .setAudioAttributes(new AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_GAME).setContentType(AudioAttributes.CONTENT_TYPE_MUSIC).build())
                .setAudioFormat(new AudioFormat.Builder().setEncoding(AudioFormat.ENCODING_PCM_16BIT).setSampleRate(rate).setChannelMask(AudioFormat.CHANNEL_OUT_MONO).build())
                .setBufferSizeInBytes(Math.max(min, frames * 2 * 2))
                .setTransferMode(AudioTrack.MODE_STREAM);
            if (Build.VERSION.SDK_INT >= 26) builder.setPerformanceMode(AudioTrack.PERFORMANCE_MODE_LOW_LATENCY);
            AudioTrack track = builder.build();
            track.play();
            return track;
        }

        @Override public void run() {
            Process.setThreadPriority(Process.THREAD_PRIORITY_URGENT_AUDIO);
            AudioTrack track = null;
            while (running) {
                try {
                    if (track == null) track = track();
                    byte[] pcm = Chordharp.fill(frames);
                    int written = track.write(pcm, 0, pcm.length);
                    if (written < 0) throw new IllegalStateException("AudioTrack.write returned " + written);
                } catch (Exception e) {
                    android.util.Log.w("gofastr-native", "chord harp audio track failed; opening a new one", e);
                    if (track != null) { track.release(); track = null; }
                    SystemClock.sleep(100);
                }
            }
            if (track != null) { track.stop(); track.release(); }
        }

        /** Stops after the buffer being written, then frees the track. */
        void finish() {
            running = false;
            try { join(500); } catch (InterruptedException e) { Thread.currentThread().interrupt(); }
        }
    }
}
