package dev.gofastr.nativeui;

import android.Manifest;
import android.content.Context;
import android.content.pm.PackageManager;
import android.hardware.GeomagneticField;
import android.hardware.Sensor;
import android.hardware.SensorEvent;
import android.hardware.SensorEventListener;
import android.hardware.SensorManager;
import android.location.Location;
import android.location.LocationListener;
import android.location.LocationManager;
import android.os.Build;
import android.os.Bundle;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.SystemClock;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;
import java.util.function.BiConsumer;

/**
 * Location fixes, camera orientation and acceleration for one capture session. Callbacks run
 * on a private thread and report JSON to the sink as (kind, payload).
 */
final class GofastrCaptureSensors implements SensorEventListener, LocationListener {
    private final Context context;
    private final boolean front;
    private final BiConsumer<String, String> sink;
    private final HandlerThread thread = new HandlerThread("gofastr-capture-sensors");
    private Handler handler;
    private SensorManager sensors;
    private LocationManager locations;
    private boolean locationActive, motionActive, accelerationActive;
    /** Gravity estimate for devices without a linear acceleration sensor. */
    private final float[] gravity = new float[3];
    private boolean gravitySeeded;
    private volatile GeomagneticField field;
    private volatile Location last;
    private final float[] matrix = new float[9];
    private final float[] quaternion = new float[4];

    GofastrCaptureSensors(Context context, boolean front, BiConsumer<String, String> sink) {
        this.context = context;
        this.front = front;
        this.sink = sink;
    }

    /** Starts what was asked for and permitted. Returns {location, motion, acceleration} actually started. */
    boolean[] start(boolean location, boolean motion, boolean gyroscope, boolean acceleration, int rate) {
        thread.start();
        handler = new Handler(thread.getLooper());
        if (location && granted(Manifest.permission.ACCESS_FINE_LOCATION) || location && granted(Manifest.permission.ACCESS_COARSE_LOCATION)) {
            locations = (LocationManager) context.getSystemService(Context.LOCATION_SERVICE);
            try {
                for (String provider : new String[] {LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER}) {
                    if (locations.getAllProviders().contains(provider)) {
                        locations.requestLocationUpdates(provider, 1000L, 0f, this, thread.getLooper());
                        locationActive = true;
                    }
                }
                Location known = locations.getLastKnownLocation(LocationManager.GPS_PROVIDER);
                if (known != null) updateField(known);
            } catch (SecurityException | IllegalArgumentException e) {
                locationActive = false;
            }
        }
        if (motion || gyroscope || acceleration) {
            sensors = (SensorManager) context.getSystemService(Context.SENSOR_SERVICE);
            int period = 1_000_000 / Math.max(1, Math.min(200, rate));
            Sensor vector = sensors.getDefaultSensor(Sensor.TYPE_ROTATION_VECTOR);
            if (motion && vector != null) motionActive = sensors.registerListener(this, vector, period, handler);
            Sensor gyro = sensors.getDefaultSensor(Sensor.TYPE_GYROSCOPE);
            if (gyroscope && gyro != null) motionActive |= sensors.registerListener(this, gyro, period, handler);
            if (acceleration) {
                Sensor linear = sensors.getDefaultSensor(Sensor.TYPE_LINEAR_ACCELERATION);
                if (linear == null) linear = sensors.getDefaultSensor(Sensor.TYPE_ACCELEROMETER);
                if (linear != null) accelerationActive = sensors.registerListener(this, linear, period, handler);
            }
        }
        return new boolean[] {locationActive, motionActive, accelerationActive};
    }

    void stop() {
        if (sensors != null) sensors.unregisterListener(this);
        if (locations != null) {
            try { locations.removeUpdates(this); } catch (SecurityException ignored) {}
        }
        thread.quitSafely();
    }

    private boolean granted(String permission) {
        return Build.VERSION.SDK_INT < 23 || context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED;
    }

    private void updateField(Location location) {
        last = location;
        field = new GeomagneticField((float) location.getLatitude(), (float) location.getLongitude(),
                location.hasAltitude() ? (float) location.getAltitude() : 0f, location.getTime());
    }

    @Override public void onLocationChanged(Location location) {
        updateField(location);
        StringBuilder json = new StringBuilder(256);
        json.append("{\"clock\":").append(location.getElapsedRealtimeNanos())
            .append(",\"time\":\"").append(iso(location.getTime())).append('"')
            .append(",\"provider\":\"").append(location.getProvider() == null ? "" : location.getProvider().replaceAll("[^a-z_]", "")).append('"')
            .append(",\"latitude\":").append(location.getLatitude())
            .append(",\"longitude\":").append(location.getLongitude())
            .append(",\"accuracy\":").append(location.hasAccuracy() ? location.getAccuracy() : -1);
        if (location.hasAltitude()) json.append(",\"altitude\":").append(location.getAltitude());
        if (location.hasSpeed()) json.append(",\"speed\":").append(location.getSpeed());
        if (location.hasBearing()) json.append(",\"bearing\":").append(location.getBearing());
        if (Build.VERSION.SDK_INT >= 26) {
            if (location.hasVerticalAccuracy()) json.append(",\"verticalAccuracy\":").append(location.getVerticalAccuracyMeters());
            if (location.hasSpeedAccuracy()) json.append(",\"speedAccuracy\":").append(location.getSpeedAccuracyMetersPerSecond());
            if (location.hasBearingAccuracy()) json.append(",\"bearingAccuracy\":").append(location.getBearingAccuracyDegrees());
        }
        sink.accept("location", json.append('}').toString());
    }

    @Override public void onStatusChanged(String provider, int status, Bundle extras) {}
    @Override public void onProviderEnabled(String provider) {}
    @Override public void onProviderDisabled(String provider) {}

    @Override public void onSensorChanged(SensorEvent event) {
        int type = event.sensor.getType();
        if (type == Sensor.TYPE_LINEAR_ACCELERATION) {
            acceleration(event.timestamp, event.values[0], event.values[1], event.values[2]);
            return;
        }
        if (type == Sensor.TYPE_ACCELEROMETER) {
            // No fused sensor: a low-pass filter estimates gravity to remove.
            for (int i = 0; i < 3; i++) gravity[i] = gravitySeeded ? 0.9f * gravity[i] + 0.1f * event.values[i] : event.values[i];
            gravitySeeded = true;
            acceleration(event.timestamp, event.values[0] - gravity[0], event.values[1] - gravity[1], event.values[2] - gravity[2]);
            return;
        }
        if (type == Sensor.TYPE_GYROSCOPE) {
            sink.accept("rotation", "{\"clock\":" + event.timestamp + ",\"x\":" + event.values[0] + ",\"y\":" + event.values[1] + ",\"z\":" + event.values[2] + "}");
            return;
        }
        SensorManager.getRotationMatrixFromVector(matrix, event.values);
        SensorManager.getQuaternionFromVector(quaternion, event.values);
        // matrix maps device axes to world east-north-up. The back camera looks
        // along device -Z, the front camera along +Z.
        double sign = front ? 1 : -1;
        double east = sign * matrix[2], north = sign * matrix[5], up = sign * matrix[8];
        double azimuth = (Math.toDegrees(Math.atan2(east, north)) + 360) % 360;
        double pitch = Math.toDegrees(Math.asin(Math.max(-1, Math.min(1, up))));
        double roll = Math.toDegrees(Math.atan2(matrix[6], matrix[7]));
        StringBuilder json = new StringBuilder(256);
        json.append("{\"clock\":").append(event.timestamp)
            .append(",\"azimuth\":").append(round(azimuth))
            .append(",\"pitch\":").append(round(pitch))
            .append(",\"roll\":").append(round(roll));
        GeomagneticField declination = field;
        if (declination != null) json.append(",\"heading\":").append(round((azimuth + declination.getDeclination() + 360) % 360));
        if (event.values.length > 4 && event.values[4] >= 0) json.append(",\"accuracy\":").append(round(Math.toDegrees(event.values[4])));
        // getQuaternionFromVector returns w, x, y, z.
        json.append(",\"quaternion\":[").append(quaternion[1]).append(',').append(quaternion[2]).append(',')
            .append(quaternion[3]).append(',').append(quaternion[0]).append("]}");
        sink.accept("orientation", json.toString());
    }

    private void acceleration(long clock, float x, float y, float z) {
        sink.accept("acceleration", "{\"clock\":" + clock + ",\"x\":" + round(x) + ",\"y\":" + round(y) + ",\"z\":" + round(z) + "}");
    }

    @Override public void onAccuracyChanged(Sensor sensor, int accuracy) {}

    private static double round(double value) { return Math.round(value * 100) / 100.0; }

    static String iso(long millis) {
        SimpleDateFormat format = new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US);
        format.setTimeZone(TimeZone.getTimeZone("UTC"));
        return format.format(new Date(millis));
    }

    static long now() { return SystemClock.elapsedRealtimeNanos(); }
}
