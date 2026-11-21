package dev.gofastr.nativeui;

import android.app.AlertDialog;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.net.Uri;
import barcodebind.Barcodebind;
import java.io.FileInputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.function.Consumer;
import org.json.JSONObject;

/** Serves scanner/v1 on Android by decoding one still photo or selected image in Go. */
public final class GofastrBarcode implements Extensions.ServiceFactory {
    private static final int MAX_IMAGE_BYTES = 16 << 20;
    private static final long MAX_SOURCE_PIXELS = 100_000_000L;

    @Override public Map<String,String> capabilities(GofastrActivity host) {
        Map<String,String> capabilities = new LinkedHashMap<>();
        capabilities.put("scanner/v1", "available");
        return capabilities;
    }

    @Override public void perform(GofastrActivity host, JSONObject request, Consumer<JSONObject> result) throws Exception {
        String scan = request.toString();
        if (!Barcodebind.supports(scan)) {
            result.accept(Services.outcome("unsupported", "Android cannot read the requested code formats."));
            return;
        }
        boolean camera = host.canUseCamera() && host.getPackageManager().hasSystemFeature(PackageManager.FEATURE_CAMERA_ANY);
        String[] choices = camera ? new String[]{"Take photo", "Choose image"} : new String[]{"Choose image"};
        boolean[] chosen = {false};
        AlertDialog dialog = new AlertDialog.Builder(host).setTitle(request.optString("title"))
            .setItems(choices, (d, which) -> {
                chosen[0] = true;
                start(host, scan, camera && which == 0, result);
            })
            .setNegativeButton("Cancel", null)
            .create();
        dialog.setOnDismissListener(d -> { if (!chosen[0]) result.accept(Services.outcome("cancelled", "Scan cancelled.")); });
        dialog.show();
    }

    private static void start(GofastrActivity host, String scan, boolean camera, Consumer<JSONObject> result) {
        Services services = host.deviceServices();
        Consumer<Uri> selected = uri -> host.workers.execute(() -> {
            JSONObject outcome;
            try {
                InputStream input = camera ? new FileInputStream(uri.getPath()) : host.getContentResolver().openInputStream(uri);
                String decoded = decode(scan, Services.read(input, MAX_IMAGE_BYTES));
                outcome = decoded.isEmpty()
                    ? Services.outcome("failed", "No supported code was found in the image.")
                    : JSON.object("status", "completed", "scan", new JSONObject(decoded));
            } catch (SecurityException e) {
                outcome = Services.outcome("denied", "Access to the image was denied.");
            } catch (Exception e) {
                outcome = Services.outcome("failed", "The image could not be scanned.");
            }
            JSONObject settled = outcome;
            host.main.post(() -> {
                if (camera) services.releaseCapture();
                result.accept(settled);
            });
        });
        if (camera) services.capture(selected);
        else services.choose(new Intent(Intent.ACTION_OPEN_DOCUMENT).addCategory(Intent.CATEGORY_OPENABLE).setType("image/*"), selected);
    }

    private static String decode(String scan, byte[] data) throws Exception {
        BitmapFactory.Options options = new BitmapFactory.Options();
        options.inJustDecodeBounds = true;
        BitmapFactory.decodeByteArray(data, 0, data.length, options);
        int width = options.outWidth, height = options.outHeight;
        if (width < 1 || height < 1 || (long) width * height > MAX_SOURCE_PIXELS) throw new IOException("Invalid image dimensions");
        long limit = Barcodebind.maxDimension();
        int sample = 1;
        while ((Math.max(width, height) + (long) sample - 1) / sample > limit) sample *= 2;
        options.inJustDecodeBounds = false;
        options.inSampleSize = sample;
        options.inPreferredConfig = Bitmap.Config.ARGB_8888;
        Bitmap bitmap = BitmapFactory.decodeByteArray(data, 0, data.length, options);
        if (bitmap == null) throw new IOException("Invalid image");
        try {
            width = bitmap.getWidth();
            height = bitmap.getHeight();
            if (width > limit || height > limit) throw new IOException("Image exceeds decode dimensions");
            byte[] pixels = new byte[width * height];
            int[] row = new int[width];
            for (int y = 0; y < height; y++) {
                bitmap.getPixels(row, 0, width, 0, y, width, 1);
                for (int x = 0; x < width; x++) {
                    int color = row[x], opacity = color >>> 24;
                    int gray = (306 * ((color >>> 16) & 255) + 601 * ((color >>> 8) & 255) + 117 * (color & 255)) >>> 10;
                    pixels[y * width + x] = (byte) ((gray * opacity + 255 * (255 - opacity)) / 255);
                }
            }
            return Barcodebind.decode(scan, pixels, width, height);
        } finally {
            bitmap.recycle();
        }
    }
}
