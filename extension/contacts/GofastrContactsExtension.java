package dev.gofastr.nativeui;

import android.content.Intent;
import android.database.Cursor;
import android.provider.ContactsContract.CommonDataKinds.Phone;
import android.provider.ContactsContract.CommonDataKinds.Email;
import org.json.JSONObject;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import java.util.Map;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.Consumer;

/** Reads only the row explicitly granted by the system picker. */
public final class GofastrContactsExtension implements Extensions.FileFactory {
    private static Intent picker(boolean phone) {
        return new Intent(Intent.ACTION_PICK, phone ? Phone.CONTENT_URI : Email.CONTENT_URI);
    }
    @Override public Map<String,String> capabilities(GofastrActivity host) {
        boolean available = picker(true).resolveActivity(host.getPackageManager()) != null
            || picker(false).resolveActivity(host.getPackageManager()) != null;
        return Collections.singletonMap("contacts/v1", available ? "available" : "unavailable");
    }
    private static String text(String value, int maximum) throws IOException {
        if (value == null) return "";
        if (value.getBytes(StandardCharsets.UTF_8).length > maximum) throw new IOException("Contact text too long");
        for (int i=0; i<value.length(); i++) if (Character.isISOControl(value.charAt(i))) throw new IOException("Invalid contact text");
        return value;
    }
    @Override public void acquire(GofastrActivity host, JSONObject request, Consumer<JSONObject> result) throws Exception {
        String kind = request.getString("kind"), title = request.getString("title");
        if ((!kind.equals("phone") && !kind.equals("email")) || title.isEmpty() || title.getBytes(StandardCharsets.UTF_8).length > 200) throw new IOException("Invalid contact request");
        boolean phone = kind.equals("phone");
        AtomicBoolean settled = new AtomicBoolean();
        Consumer<JSONObject> finish = outcome -> host.main.post(() -> { if (settled.compareAndSet(false,true)) result.accept(outcome); });
        Intent intent = picker(phone);
        if (intent.resolveActivity(host.getPackageManager()) == null) { finish.accept(JSON.object("status","unsupported")); return; }
        host.deviceServices().choose(intent, uri -> host.workers.execute(() -> {
            JSONObject outcome;
            try {
                if (!"content".equals(uri.getScheme())) throw new IOException("Invalid contact URI");
                String column = phone ? Phone.NUMBER : Email.ADDRESS;
                try (Cursor cursor = host.getContentResolver().query(uri, new String[]{Phone.DISPLAY_NAME, column}, null, null, null)) {
                    if (cursor == null || !cursor.moveToFirst()) throw new IOException("No selected contact row");
                    String name = text(cursor.getString(0),512), value = text(cursor.getString(1),phone ? 256 : 320);
                    if (value.trim().isEmpty()) throw new IOException("Empty contact detail");
                    byte[] bytes = JSON.object("name",name,"phone",phone ? value : "","email",phone ? "" : value).toString().getBytes(StandardCharsets.UTF_8);
                    if (bytes.length > 4096) throw new IOException("Contact exceeds byte limit");
                    outcome = JSON.object("status","completed","file",Services.document("Contact.json","application/json",bytes));
                }
            } catch (SecurityException e) { outcome = JSON.object("status","denied"); }
            catch (Exception e) { outcome = JSON.object("status","failed"); }
            finish.accept(outcome);
        }));
    }
}
