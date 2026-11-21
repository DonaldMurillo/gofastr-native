package dev.gofastr.nativeui;

import android.app.AlertDialog;
import android.content.Context;
import android.content.Intent;
import android.media.AudioAttributes;
import android.os.Bundle;
import android.speech.RecognitionListener;
import android.speech.RecognizerIntent;
import android.speech.SpeechRecognizer;
import android.speech.tts.TextToSpeech;
import android.speech.tts.UtteranceProgressListener;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.Locale;
import java.util.function.Consumer;
import org.json.JSONObject;

/** Owns one foreground utterance. Platform callbacks from retired sessions are ignored. */
final class Speech {
    private static final int MAX_TEXT_BYTES = 4000;
    private static final String UTTERANCE_ID = "gofastr-speech";
    private final GofastrActivity host;
    private Session active;

    Speech(GofastrActivity host) { this.host = host; }

    static boolean canSpeak(Context context) {
        return !context.getPackageManager().queryIntentServices(
            new Intent(TextToSpeech.Engine.INTENT_ACTION_TTS_SERVICE), 0).isEmpty();
    }

    void run(JSONObject request, Consumer<JSONObject> done) {
        Session session = new Session(done);
        active = session;
        try {
            String language = request.optString("language");
            if ("speak".equals(request.optString("operation"))) {
                session.speak(request.optString("text"), language);
            } else if ("recognize".equals(request.optString("operation"))) {
                session.recognize(language);
            } else {
                session.finish("unsupported", "This speech operation is unavailable.");
            }
        } catch (SecurityException e) {
            session.finish("denied", "Speech access was denied.");
        } catch (RuntimeException e) {
            session.finish("failed", "The speech provider could not start.");
        }
    }

    void cancel() {
        if (active != null) active.finish("cancelled", "Speech cancelled.");
    }

    private final class Session {
        private final Consumer<JSONObject> done;
        private final Runnable timeout = () -> finish("failed", "The speech operation timed out.");
        private AlertDialog dialog;
        private TextToSpeech tts;
        private SpeechRecognizer recognizer;

        Session(Consumer<JSONObject> done) { this.done = done; }

        void speak(String text, String language) {
            if (!canSpeak(host)) {
                finish("unsupported", "No text-to-speech engine is installed.");
                return;
            }
            if (text.length() > TextToSpeech.getMaxSpeechInputLength() || !validText(text)) {
                finish("failed", "The text is empty or exceeds the speech limit.");
                return;
            }
            show("Text to speech", "Preparing speech…", 300_000L);
            tts = new TextToSpeech(host, status -> host.main.post(() -> startSpeaking(status, text, language)));
        }

        private void startSpeaking(int status, String text, String language) {
            if (active != this) return;
            try {
                if (status != TextToSpeech.SUCCESS) {
                    finish("failed", "The text-to-speech engine could not initialize.");
                    return;
                }
                if (!language.isEmpty() && tts.setLanguage(Locale.forLanguageTag(language)) < 0) {
                    finish("unsupported", "The text-to-speech engine does not support this language.");
                    return;
                }
                tts.setAudioAttributes(new AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_MEDIA)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build());
                int listenerStatus = tts.setOnUtteranceProgressListener(new UtteranceProgressListener() {
                    @Override public void onStart(String id) {}
                    @Override public void onDone(String id) { settled(id, "completed", "Speech finished."); }
                    @Override public void onError(String id) { settled(id, "failed", "The text-to-speech engine could not speak."); }
                    @Override public void onStop(String id, boolean interrupted) { settled(id, "cancelled", "Speech cancelled."); }
                });
                if (listenerStatus != TextToSpeech.SUCCESS ||
                        tts.speak(text, TextToSpeech.QUEUE_FLUSH, null, UTTERANCE_ID) != TextToSpeech.SUCCESS) {
                    finish("failed", "The text-to-speech engine could not speak.");
                    return;
                }
                dialog.setMessage("Speaking…");
            } catch (RuntimeException e) {
                finish("failed", "The text-to-speech engine could not speak.");
            }
        }

        private void settled(String id, String status, String message) {
            if (UTTERANCE_ID.equals(id)) host.main.post(() -> finish(status, message));
        }

        void recognize(String language) {
            if (!SpeechRecognizer.isRecognitionAvailable(host)) {
                finish("unsupported", "No speech recognizer is installed.");
                return;
            }
            recognizer = SpeechRecognizer.createSpeechRecognizer(host);
            recognizer.setRecognitionListener(new RecognitionListener() {
                @Override public void onResults(Bundle results) {
                    if (active != Session.this) return;
                    String transcript = bestTranscript(results);
                    if (validText(transcript)) {
                        finish(JSON.object("status", "completed", "transcript", transcript));
                    } else {
                        finish("failed", "No usable transcript was returned.");
                    }
                }
                @Override public void onError(int error) {
                    switch (error) {
                    case SpeechRecognizer.ERROR_INSUFFICIENT_PERMISSIONS:
                        finish("denied", "Microphone access was denied.");
                        break;
                    case SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED:
                    case SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE:
                        finish("unsupported", "The recognizer does not support this language.");
                        break;
                    case SpeechRecognizer.ERROR_NO_MATCH:
                    case SpeechRecognizer.ERROR_SPEECH_TIMEOUT:
                        finish("failed", "No speech was recognized.");
                        break;
                    default:
                        finish("failed", "The speech recognizer could not complete.");
                    }
                }
                @Override public void onReadyForSpeech(Bundle params) {}
                @Override public void onBeginningOfSpeech() {}
                @Override public void onRmsChanged(float rmsdB) {}
                @Override public void onBufferReceived(byte[] buffer) {}
                @Override public void onEndOfSpeech() {}
                @Override public void onPartialResults(Bundle results) {}
                @Override public void onEvent(int event, Bundle params) {}
            });
            Intent intent = new Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH)
                .putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
                .putExtra(RecognizerIntent.EXTRA_MAX_RESULTS, 1);
            if (!language.isEmpty()) intent.putExtra(RecognizerIntent.EXTRA_LANGUAGE, language);
            show("Speech recognition", "Listening… Your Android speech provider may process audio online.", 30_000L);
            recognizer.startListening(intent);
        }

        private void show(String title, String message, long limit) {
            dialog = new AlertDialog.Builder(host).setTitle(title).setMessage(message)
                .setNegativeButton("Cancel", (dialog, button) -> finish("cancelled", "Speech cancelled."))
                .setOnCancelListener(dialog -> finish("cancelled", "Speech cancelled.")).show();
            host.main.postDelayed(timeout, limit);
        }

        private void finish(String status, String message) {
            finish(Services.outcome(status, message));
        }

        private void finish(JSONObject outcome) {
            if (active != this) return;
            active = null;
            host.main.removeCallbacks(timeout);
            if (dialog != null) dialog.dismiss();
            if (recognizer != null) {
                recognizer.cancel();
                recognizer.destroy();
            }
            if (tts != null) {
                tts.stop();
                tts.shutdown();
            }
            done.accept(outcome);
        }
    }

    private static String bestTranscript(Bundle results) {
        if (results == null) return null;
        try {
            ArrayList<String> matches = results.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION);
            return matches == null || matches.isEmpty() ? null : matches.get(0);
        } catch (RuntimeException invalidResult) {
            return null;
        }
    }

    private static boolean validText(String text) {
        if (text == null || text.length() > MAX_TEXT_BYTES ||
                text.getBytes(StandardCharsets.UTF_8).length > MAX_TEXT_BYTES) return false;
        boolean nonblank = false;
        for (int i = 0; i < text.length();) {
            int c = text.codePointAt(i);
            if (Character.getType(c) == Character.SURROGATE ||
                    (Character.isISOControl(c) && c != '\n' && c != '\t')) return false;
            if (!Character.isWhitespace(c) && !Character.isSpaceChar(c)) nonblank = true;
            i += Character.charCount(c);
        }
        return nonblank;
    }
}
