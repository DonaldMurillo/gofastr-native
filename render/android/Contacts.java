package dev.gofastr.nativeui;

import android.content.Context;
import android.content.Intent;
import android.database.Cursor;
import android.net.Uri;
import android.provider.ContactsContract.CommonDataKinds.Email;
import android.provider.ContactsContract.CommonDataKinds.Phone;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.IOException;
import java.nio.charset.StandardCharsets;

/**
 * contacts/v1 through the system row picker. Only the row the picker grants is
 * read, on a worker; READ_CONTACTS is never requested. Android serves single
 * selection of one phone number or one email address.
 */
final class Contacts {
    private static final int MAX_TEXT = 512;
    private Contacts() {}

    static Intent picker(String field) {
        return new Intent(Intent.ACTION_PICK, "email".equals(field) ? Email.CONTENT_URI : Phone.CONTENT_URI);
    }

    static boolean available(Context context) {
        return picker("phone").resolveActivity(context.getPackageManager()) != null
            || picker("email").resolveActivity(context.getPackageManager()) != null;
    }

    /** The one field this adapter can serve for request, or null. */
    static String field(JSONObject request) {
        JSONArray fields = JSON.array(request, "fields");
        if (request.optBoolean("multiple") || fields.length() != 1) return null;
        String field = fields.optString(0);
        return field.equals("phone") || field.equals("email") ? field : null;
    }

    private static String text(String value) throws IOException {
        if (value == null) return "";
        if (value.getBytes(StandardCharsets.UTF_8).length > MAX_TEXT) throw new IOException("Contact text too long");
        for (int i = 0; i < value.length(); i++) if (Character.isISOControl(value.charAt(i))) throw new IOException("Invalid contact text");
        return value;
    }

    /** Reads the granted row as a contacts/v1 selection with one contact. */
    static JSONObject read(Context context, Uri uri, String field) throws IOException {
        if (uri == null || !"content".equals(uri.getScheme())) throw new IOException("Invalid contact URI");
        boolean phone = field.equals("phone");
        String[] projection = phone
            ? new String[]{Phone.DISPLAY_NAME, Phone.NUMBER, Phone.TYPE, Phone.LABEL, Phone.LOOKUP_KEY}
            : new String[]{Email.DISPLAY_NAME, Email.ADDRESS, Email.TYPE, Email.LABEL, Email.LOOKUP_KEY};
        try (Cursor cursor = context.getContentResolver().query(uri, projection, null, null, null)) {
            if (cursor == null || !cursor.moveToFirst()) throw new IOException("No selected contact row");
            String name = text(cursor.getString(0)), value = text(cursor.getString(1));
            if (value.trim().isEmpty()) throw new IOException("Empty contact detail");
            int type = cursor.isNull(2) ? 0 : cursor.getInt(2);
            CharSequence label = phone
                ? Phone.getTypeLabel(context.getResources(), type, cursor.getString(3))
                : Email.getTypeLabel(context.getResources(), type, cursor.getString(3));
            String id = cursor.getString(4);
            if (id == null || id.isEmpty() || id.getBytes(StandardCharsets.UTF_8).length > MAX_TEXT) id = uri.getLastPathSegment();
            id = text(id);
            if (id.isEmpty()) throw new IOException("No contact identifier");
            JSONObject contact = JSON.object("id", id, "givenName", name, "familyName", "");
            GofastrActivity.put(contact, phone ? "phones" : "emails", new JSONArray().put(JSON.object("label", text(label == null ? "" : label.toString()), "value", value)));
            return JSON.object("contacts", new JSONArray().put(contact));
        }
    }
}
