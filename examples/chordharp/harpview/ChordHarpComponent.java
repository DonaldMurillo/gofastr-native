package dev.gofastr.nativeui;

import org.json.JSONObject;
import java.util.Collections;
import java.util.Map;
import java.util.function.Consumer;

/** Mounts the chord harp instrument. It emits no events: it never navigates. */
public final class ChordHarpComponent implements Extensions.ComponentFactory {
    @Override public Map<String,String> capabilities(GofastrActivity host) {
        return Collections.singletonMap("dev.gofastr.chordharp/v1", "available");
    }
    @Override public Extensions.Component mount(GofastrActivity host, JSONObject payload, Consumer<String> emit) throws Exception {
        ChordHarpView view = new ChordHarpView(host, payload);
        return new Extensions.Component() {
            @Override public android.view.View view() { return view; }
            @Override public void update(JSONObject next, Consumer<String> emit) throws Exception { view.layout(next); }
            @Override public void suspend() { view.pauseAudio(); }
            @Override public void dispose() { view.pauseAudio(); }
        };
    }
}
