package dev.gofastr.nativeui;

import android.Manifest;
import android.content.Context;
import android.content.SharedPreferences;
import android.content.pm.PackageManager;
import android.location.Location;
import android.location.LocationListener;
import android.location.LocationManager;
import android.os.Build;
import android.os.Bundle;
import org.json.JSONArray;
import org.json.JSONObject;
import java.nio.charset.StandardCharsets;
import java.util.function.Consumer;

/**
 * One bounded foreground location operation (device/location wire contract),
 * fenced by a generation so late provider callbacks cannot settle a later one.
 */
public final class DeviceLocation {
    static final int PERMISSION_REQUEST=407;
    private static final int DEFAULT_TIMEOUT_MILLIS=15000;
    private final GofastrActivity host;
    private final LocationManager manager;
    private JSONObject request;
    private Consumer<JSONObject> completion;
    private LocationListener listener;
    private Runnable deadline;
    private JSONArray fixes=new JSONArray();
    private boolean prompting,started;
    private long generation;

    DeviceLocation(GofastrActivity host) {
        this.host=host;
        manager=(LocationManager)host.getSystemService(Context.LOCATION_SERVICE);
    }

    private boolean granted(String permission){return host.checkSelfPermission(permission)==PackageManager.PERMISSION_GRANTED;}
    private boolean precise(){return granted(Manifest.permission.ACCESS_FINE_LOCATION);}
    /** Android has no separate restricted state here; background location is never declared, so "always" is never reported. */
    String permission() {
        if(precise() || granted(Manifest.permission.ACCESS_COARSE_LOCATION))return "when_in_use";
        return preferences().getBoolean("requested",false)?"denied":"not_determined";
    }
    private SharedPreferences preferences(){return host.getSharedPreferences("gofastr.location",Context.MODE_PRIVATE);}
    private boolean enabled() {
        if(manager==null)return false;
        if(Build.VERSION.SDK_INT>=28)return manager.isLocationEnabled();
        return manager.isProviderEnabled(LocationManager.GPS_PROVIDER) || manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER);
    }

    static void validate(JSONObject request) {
        String op=request.optString("operation"),accuracy=request.optString("accuracy"),reason=request.optString("reason");
        int timeout=request.optInt("timeoutMillis"),samples=request.optInt("maxSamples"),duration=request.optInt("durationMillis");
        double distance=request.optDouble("distanceFilterMeters",0);
        if(reason.getBytes(StandardCharsets.UTF_8).length>200 || reason.matches("(?s).*\\p{Cntrl}.*") || Double.isNaN(distance) || Double.isInfinite(distance) || distance<0)
            throw new IllegalArgumentException("Invalid location reason or distance filter");
        switch(op) {
            case "status": case "authorize":
                if(!accuracy.isEmpty() || timeout!=0 || samples!=0 || duration!=0 || distance!=0 || op.equals("status") && !reason.isEmpty())throw new IllegalArgumentException("Unexpected location permission arguments");
                return;
            case "current": case "track":
                if(!(accuracy.isEmpty() || accuracy.equals("best") || accuracy.equals("balanced") || accuracy.equals("coarse")))throw new IllegalArgumentException("Invalid location accuracy");
                break;
            default: throw new IllegalArgumentException("Unknown location operation");
        }
        if(op.equals("current")) {
            if(timeout<0 || timeout>60000 || samples!=0 || duration!=0 || distance!=0)throw new IllegalArgumentException("Invalid current position bounds");
        }else if(timeout!=0 || samples<1 || samples>100 || duration<1000 || duration>120000)throw new IllegalArgumentException("Invalid tracking bounds");
    }

    void perform(JSONObject request,Consumer<JSONObject> callback) {
        validate(request);
        if(completion!=null){callback.accept(Services.outcome("failed","A location operation is already running."));return;}
        long token=++generation;
        this.request=request;completion=callback;fixes=new JSONArray();started=false;prompting=false;
        if(request.optString("operation").equals("current")) {
            int timeout=request.optInt("timeoutMillis");
            schedule(token,timeout==0?DEFAULT_TIMEOUT_MILLIS:timeout);
        }
        advance(token,true);
    }

    private void advance(long token,boolean mayPrompt) {
        if(token!=generation || completion==null)return;
        try {
            String permission=permission(),op=request.optString("operation");
            if(!enabled()){finish(token,outcome("unsupported",permission,"Location services are off."));return;}
            if(permission.equals("denied")){finish(token,outcome("denied",permission,"Location permission was denied."));return;}
            if(op.equals("status")){complete(token);return;}
            if(permission.equals("not_determined")) {
                if(!mayPrompt){cancel();return;}
                prompting=true;
                preferences().edit().putBoolean("requested",true).apply();
                host.requestPermissions(request.optString("accuracy").equals("coarse")?new String[]{Manifest.permission.ACCESS_COARSE_LOCATION}:new String[]{Manifest.permission.ACCESS_COARSE_LOCATION,Manifest.permission.ACCESS_FINE_LOCATION},PERMISSION_REQUEST);
                return;
            }
            if(op.equals("authorize")){complete(token);return;}
            if(!started)start(token,op);
        }catch(SecurityException error){finish(token,outcome("denied",permission(),"Location permission was denied."));}
        catch(Exception error){finish(token,outcome("failed",permission(),"The location read failed."));}
    }

    /** Called with the grants of PERMISSION_REQUEST. An interrupted request (no grants) cancels. */
    void permissionsResult(int[] grants) {
        if(!prompting || completion==null)return;
        prompting=false;
        if(grants.length==0){cancel();return;}
        advance(generation,false);
    }

    private String provider(String accuracy) {
        boolean gps=precise() && manager.isProviderEnabled(LocationManager.GPS_PROVIDER),network=manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER);
        boolean fused=Build.VERSION.SDK_INT>=31 && manager.hasProvider(LocationManager.FUSED_PROVIDER) && manager.isProviderEnabled(LocationManager.FUSED_PROVIDER);
        if(accuracy.equals("best")) {
            if(gps)return LocationManager.GPS_PROVIDER;
            if(fused)return LocationManager.FUSED_PROVIDER;
            if(network)return LocationManager.NETWORK_PROVIDER;
            return null;
        }
        if(network)return LocationManager.NETWORK_PROVIDER;
        if(fused)return LocationManager.FUSED_PROVIDER;
        if(gps)return LocationManager.GPS_PROVIDER;
        return null;
    }

    @SuppressWarnings("MissingPermission")
    private void start(long token,String op) {
        started=true;
        String accuracy=request.optString("accuracy");
        String provider=provider(accuracy.isEmpty()?"best":accuracy);
        if(provider==null){finish(token,outcome("failed",permission(),"No location provider is available."));return;}
        boolean track=op.equals("track");
        if(track)schedule(token,request.optInt("durationMillis"));
        listener=new LocationListener(){
            public void onLocationChanged(Location value){located(token,value);}
            public void onStatusChanged(String provider,int status,Bundle extras){}
            public void onProviderEnabled(String provider){}
            public void onProviderDisabled(String provider){}
        };
        manager.requestLocationUpdates(provider,0L,track?(float)request.optDouble("distanceFilterMeters",0):0f,listener,host.main.getLooper());
    }

    private void schedule(long token,long milliseconds) {
        if(deadline!=null)host.main.removeCallbacks(deadline);
        deadline=()->{
            if(token!=generation || completion==null)return;
            deadline=null;
            if(fixes.length()==0)finish(token,outcome("failed",permission(),"No location fix arrived in time."));
            else complete(token);
        };
        host.main.postDelayed(deadline,milliseconds);
    }

    private void located(long token,Location value) {
        if(token!=generation || completion==null || !started)return;
        JSONObject fix=fix(value);
        if(fix==null)return;
        fixes.put(fix);
        int limit=request.optString("operation").equals("current")?1:request.optInt("maxSamples");
        if(fixes.length()>=limit)complete(token);
    }

    /** Converts a platform fix to the wire shape, or null when it is unusable. Unknown details use -1. */
    static JSONObject fix(Location value) {
        if(value==null || !value.hasAccuracy())return null;
        double latitude=value.getLatitude(),longitude=value.getLongitude(),accuracy=value.getAccuracy();
        if(!finite(latitude) || Math.abs(latitude)>90 || !finite(longitude) || Math.abs(longitude)>180 || !finite(accuracy) || accuracy<0 || value.getTime()<0)return null;
        double altitude=value.hasAltitude()?value.getAltitude():0,vertical=-1,speed=-1,course=-1;
        if(!finite(altitude))return null;
        if(Build.VERSION.SDK_INT>=26 && value.hasAltitude() && value.hasVerticalAccuracy())vertical=value.getVerticalAccuracyMeters();
        if(value.hasSpeed())speed=value.getSpeed();
        if(value.hasBearing())course=value.getBearing();
        if(!finite(vertical) || vertical<0)vertical=-1;
        if(!finite(speed) || speed<0)speed=-1;
        if(!finite(course) || course<0 || course>=360)course=-1;
        return JSON.object("latitude",latitude,"longitude",longitude,"horizontalAccuracyMeters",accuracy,"altitudeMeters",altitude,
            "verticalAccuracyMeters",vertical,"speedMetersPerSecond",speed,"courseDegrees",course,"timestampMillis",value.getTime());
    }

    private void complete(long token) {
        String permission=permission();
        if(!enabled()){finish(token,outcome("unsupported",permission,"Location services are off."));return;}
        if(permission.equals("denied")){finish(token,outcome("denied",permission,"Location permission was denied."));return;}
        JSONObject location=JSON.object("permission",permission,"servicesEnabled",true,"precise",precise());
        if(fixes.length()>0)GofastrActivity.put(location,"fixes",fixes);
        finish(token,JSON.object("status","completed","location",location));
    }

    private static JSONObject outcome(String status,String permission,String message) {
        JSONObject result=Services.outcome(status,message);
        GofastrActivity.put(result,"permission",permission);
        return result;
    }
    private static boolean finite(double value){return !Double.isNaN(value) && !Double.isInfinite(value);}

    /** Leaving the foreground cancels, except while the system permission dialog itself pauses the activity. */
    void pause(){if(!prompting)cancel();}
    void cancel(){finish(generation,Services.outcome("cancelled","Location read cancelled."));}
    private void finish(long token,JSONObject result) {
        if(token!=generation || completion==null)return;
        Consumer<JSONObject> callback=completion;
        completion=null;request=null;started=false;prompting=false;fixes=new JSONArray();
        if(deadline!=null){host.main.removeCallbacks(deadline);deadline=null;}
        if(listener!=null){try{manager.removeUpdates(listener);}catch(Exception ignored){}listener=null;}
        callback.accept(result);
    }
}
