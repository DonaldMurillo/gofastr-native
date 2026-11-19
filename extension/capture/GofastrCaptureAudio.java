package dev.gofastr.nativeui;

import android.media.AudioFormat;
import android.media.AudioRecord;
import android.media.AudioTimestamp;
import android.media.MediaCodec;
import android.media.MediaCodecInfo;
import android.media.MediaFormat;
import android.media.MediaRecorder;
import android.os.Build;
import android.os.SystemClock;
import java.nio.ByteBuffer;

/**
 * Microphone capture on its own thread. PCM feeds an AAC encoder for the
 * recording and, on Android 10+, an Opus encoder for live sinks.
 */
final class GofastrCaptureAudio {
    interface Output {
        void aacFormat(MediaFormat format);
        void aac(ByteBuffer data, MediaCodec.BufferInfo info);
        void opus(byte[] data, long clock);
        /** The audio thread ended, on request or because the microphone failed. */
        void ended();
    }

    static final int RATE = 48000;
    private final Output output;
    private final boolean realtimeClock;
    private AudioRecord record;
    private MediaCodec aac, opus;
    private Thread thread;
    private volatile boolean running;

    GofastrCaptureAudio(Output output, boolean realtimeClock) {
        this.output = output;
        this.realtimeClock = realtimeClock;
    }

    /** Returns false when the microphone or every requested encoder is unavailable. */
    boolean start(boolean recordAac, boolean streamOpus) throws Exception {
        int minimum = AudioRecord.getMinBufferSize(RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT);
        if (minimum <= 0) return false;
        record = new AudioRecord(MediaRecorder.AudioSource.CAMCORDER, RATE, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT, Math.max(minimum * 2, 16384));
        if (record.getState() != AudioRecord.STATE_INITIALIZED) {
            record.release();
            record = null;
            return false;
        }
        if (recordAac) {
            MediaFormat format = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_AAC, RATE, 1);
            format.setInteger(MediaFormat.KEY_AAC_PROFILE, MediaCodecInfo.CodecProfileLevel.AACObjectLC);
            format.setInteger(MediaFormat.KEY_BIT_RATE, 96000);
            aac = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_AUDIO_AAC);
            aac.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE);
            aac.start();
        }
        if (streamOpus && Build.VERSION.SDK_INT >= 29) {
            try {
                MediaFormat format = MediaFormat.createAudioFormat(MediaFormat.MIMETYPE_AUDIO_OPUS, RATE, 1);
                format.setInteger(MediaFormat.KEY_BIT_RATE, 64000);
                opus = MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_AUDIO_OPUS);
                opus.configure(format, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE);
                opus.start();
            } catch (Exception e) {
                if (opus != null) opus.release();
                opus = null;
            }
        }
        if (aac == null && opus == null) {
            record.release();
            record = null;
            return false;
        }
        record.startRecording();
        running = true;
        thread = new Thread(this::loop, "gofastr-capture-audio");
        thread.start();
        return true;
    }

    /** Ends input, drains both encoders to end of stream and releases everything. */
    void stop() {
        running = false;
        if (thread != null) {
            try { thread.join(3000); } catch (InterruptedException ignored) { Thread.currentThread().interrupt(); }
        }
    }

    private long clock() { return realtimeClock ? SystemClock.elapsedRealtimeNanos() : System.nanoTime(); }

    private void loop() {
        byte[] pcm = new byte[RATE / 50 * 2]; // 20 ms of mono 16-bit samples
        long frames = 0;
        AudioTimestamp stamp = new AudioTimestamp();
        int timebase = realtimeClock ? AudioTimestamp.TIMEBASE_BOOTTIME : AudioTimestamp.TIMEBASE_MONOTONIC;
        MediaCodec.BufferInfo info = new MediaCodec.BufferInfo();
        long next = Long.MIN_VALUE;
        try {
            while (running) {
                int read = record.read(pcm, 0, pcm.length);
                // A negative count is an error that does not clear, such as
                // a dead audio server: end audio, and video goes on without it.
                if (read < 0) break;
                if (read == 0) continue;
                long clock;
                if (record.getTimestamp(stamp, timebase) == AudioRecord.SUCCESS) {
                    clock = stamp.nanoTime + (frames - stamp.framePosition) * 1_000_000_000L / RATE;
                } else {
                    clock = clock() - (long) (read / 2) * 1_000_000_000L / RATE;
                }
                // The two clock sources disagree by the input latency. A
                // timestamp that goes back makes the muxer reject the track,
                // so each buffer starts no earlier than the last one ended.
                clock = Math.max(clock, next);
                next = clock + (long) (read / 2) * 1_000_000_000L / RATE;
                frames += read / 2;
                if (aac != null) queue(aac, pcm, read, clock, false);
                if (opus != null) queue(opus, pcm, read, clock, false);
                drain(aac, info, false);
                drain(opus, info, false);
            }
            long clock = Math.max(clock(), next);
            if (aac != null) queue(aac, pcm, 0, clock, true);
            if (opus != null) queue(opus, pcm, 0, clock, true);
            drain(aac, info, true);
            drain(opus, info, true);
        } catch (Exception ignored) {
            // A failed microphone ends audio; video continues.
        } finally {
            try { record.stop(); } catch (Exception ignored) {}
            record.release();
            if (aac != null) { try { aac.stop(); } catch (Exception ignored) {} aac.release(); }
            if (opus != null) { try { opus.stop(); } catch (Exception ignored) {} opus.release(); }
            output.ended();
        }
    }

    private static void queue(MediaCodec codec, byte[] pcm, int length, long clock, boolean end) {
        int index = codec.dequeueInputBuffer(end ? 100_000 : 10_000);
        if (index < 0) return;
        ByteBuffer buffer = codec.getInputBuffer(index);
        buffer.clear();
        int size = Math.min(length, buffer.remaining());
        buffer.put(pcm, 0, size);
        codec.queueInputBuffer(index, 0, size, clock / 1000, end ? MediaCodec.BUFFER_FLAG_END_OF_STREAM : 0);
    }

    private void drain(MediaCodec codec, MediaCodec.BufferInfo info, boolean untilEnd) {
        if (codec == null) return;
        long deadline = SystemClock.uptimeMillis() + 2000;
        while (true) {
            int index = codec.dequeueOutputBuffer(info, untilEnd ? 10_000 : 0);
            if (index == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                if (codec == aac) output.aacFormat(codec.getOutputFormat());
                continue;
            }
            if (index < 0) {
                if (!untilEnd || SystemClock.uptimeMillis() > deadline) return;
                continue;
            }
            ByteBuffer data = codec.getOutputBuffer(index);
            boolean config = (info.flags & MediaCodec.BUFFER_FLAG_CODEC_CONFIG) != 0;
            if (info.size > 0 && !config && data != null) {
                data.position(info.offset).limit(info.offset + info.size);
                if (codec == aac) {
                    output.aac(data, info);
                } else {
                    byte[] bytes = new byte[info.size];
                    data.get(bytes);
                    output.opus(bytes, info.presentationTimeUs * 1000);
                }
            }
            codec.releaseOutputBuffer(index, false);
            if ((info.flags & MediaCodec.BUFFER_FLAG_END_OF_STREAM) != 0) return;
        }
    }
}
