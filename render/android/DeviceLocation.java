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
import android.os.CancellationSignal;
import android.os.SystemClock;
import org.json.JSONObject;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.TimeZone;
import java.util.function.Consumer;

/** One foreground location read, fenced against late provider callbacks. */
public final class DeviceLocation {
    private final GofastrActivity host;
    private final LocationManager manager;
    private Consumer<JSONObject> completion;
    private CancellationSignal cancellation;
    private LocationListener listener;
    private Runnable timeout;
    private long generation;

    DeviceLocation(GofastrActivity host) {
        this.host=host;
        manager=(LocationManager)host.getSystemService(Context.LOCATION_SERVICE);
    }

    String permission() {
        if(host.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION)==PackageManager.PERMISSION_GRANTED)return "precise";
        if(host.checkSelfPermission(Manifest.permission.ACCESS_COARSE_LOCATION)==PackageManager.PERMISSION_GRANTED)return "approximate";
        return preferences().getBoolean("requested",false)?"denied":"undetermined";
    }
    private SharedPreferences preferences(){return host.getSharedPreferences("gofastr.location",Context.MODE_PRIVATE);}
    void requested(){preferences().edit().putBoolean("requested",true).apply();}
    private boolean enabled() {
        if(manager==null)return false;
        if(Build.VERSION.SDK_INT>=28)return manager.isLocationEnabled();
        return manager.isProviderEnabled(LocationManager.GPS_PROVIDER) || manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER);
    }
    private JSONObject state(){return JSON.object("permission",permission(),"servicesEnabled",enabled());}

    static void validate(JSONObject request) {
        String op=request.optString("operation"),accuracy=request.optString("accuracy");
        int age=request.optInt("maxAgeSeconds"),seconds=request.optInt("timeoutSeconds");
        if(op.equals("status")) {
            if(!accuracy.isEmpty() || age!=0 || seconds!=0)throw new IllegalArgumentException("Status has no arguments");
        }else if(!op.equals("current") || !(accuracy.equals("precise") || accuracy.equals("approximate")) || age<0 || age>600 || seconds<0 || seconds>60)throw new IllegalArgumentException("Invalid location request");
    }

    void perform(JSONObject request,Consumer<JSONObject> callback) {
        validate(request);
        long token=++generation;
        completion=callback;
        try {
            if(request.optString("operation").equals("status")){finish(token,JSON.object("status","completed","location",state()));return;}
            if(permission().equals("denied") || permission().equals("undetermined")){finish(token,Services.outcome("denied","Location permission was denied."));return;}
            if(!enabled()){finish(token,Services.outcome("failed","Location services are off."));return;}
            int age=request.optInt("maxAgeSeconds"),seconds=request.optInt("timeoutSeconds",30);
            if(seconds==0)seconds=30;
            Location cached=null;
            if(age>0)for(String provider:new String[]{LocationManager.GPS_PROVIDER,LocationManager.NETWORK_PROVIDER,LocationManager.PASSIVE_PROVIDER}) {
                try {
                    Location candidate=manager.getLastKnownLocation(provider);
                    long elapsed=candidate==null?-1:SystemClock.elapsedRealtimeNanos()-candidate.getElapsedRealtimeNanos();
                    if(candidate!=null && candidate.hasAccuracy() && elapsed>=0 && elapsed<=age*1000000000L && (cached==null || candidate.getElapsedRealtimeNanos()>cached.getElapsedRealtimeNanos()))cached=candidate;
                }catch(SecurityException | IllegalArgumentException ignored){}
            }
            if(cached!=null){located(token,cached);return;}
            String provider=null;
            boolean precise=request.optString("accuracy").equals("precise") && permission().equals("precise");
            if(precise && manager.isProviderEnabled(LocationManager.GPS_PROVIDER))provider=LocationManager.GPS_PROVIDER;
            else if(manager.isProviderEnabled(LocationManager.NETWORK_PROVIDER))provider=LocationManager.NETWORK_PROVIDER;
            else if(manager.isProviderEnabled(LocationManager.GPS_PROVIDER))provider=LocationManager.GPS_PROVIDER;
            if(provider==null){finish(token,Services.outcome("failed","Location services are off."));return;}
            timeout=()->finish(token,Services.outcome("failed","No location fix arrived in time."));
            host.main.postDelayed(timeout,seconds*1000L);
            if(Build.VERSION.SDK_INT>=30) {
                cancellation=new CancellationSignal();
                long oldest=SystemClock.elapsedRealtimeNanos()-age*1000000000L;
                String source=provider;
                manager.getCurrentLocation(provider,cancellation,host::runOnUiThread,value->{
                    if(value!=null && value.getElapsedRealtimeNanos()<oldest && token==generation && completion!=null) {
                        try{single(token,source);}catch(SecurityException error){finish(token,Services.outcome("denied","Location permission was denied."));}
                        return;
                    }
                    located(token,value);
                });
            }else single(token,provider);
        }catch(SecurityException error){finish(token,Services.outcome("denied","Location permission was denied."));}
        catch(Exception error){finish(token,Services.outcome("failed","The location read failed."));}
    }

    @SuppressWarnings("deprecation")
    private void single(long token,String provider) {
        listener=new LocationListener(){
            public void onLocationChanged(Location value){located(token,value);}
            public void onStatusChanged(String provider,int status,Bundle extras){}
            public void onProviderEnabled(String provider){}
            public void onProviderDisabled(String provider){}
        };
        manager.requestSingleUpdate(provider,listener,host.main.getLooper());
    }

    private void located(long token,Location value) {
        if(token!=generation || completion==null)return;
        if(value==null || !value.hasAccuracy()){finish(token,Services.outcome("failed","No location fix arrived in time."));return;}
        if(!finite(value.getLatitude()) || Math.abs(value.getLatitude())>90 || !finite(value.getLongitude()) || Math.abs(value.getLongitude())>180 || !finite(value.getAccuracy()) || value.getAccuracy()<0 ||
            value.hasAltitude() && !finite(value.getAltitude()) || value.hasSpeed() && (!finite(value.getSpeed()) || value.getSpeed()<0) || value.hasBearing() && (!finite(value.getBearing()) || value.getBearing()<0 || value.getBearing()>=360)) {
            finish(token,Services.outcome("failed","The location fix was invalid."));return;
        }
        if(!permission().equals("precise") && !permission().equals("approximate")){finish(token,Services.outcome("denied","Location permission was denied."));return;}
        try {
            SimpleDateFormat format=new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'",Locale.ROOT);format.setTimeZone(TimeZone.getTimeZone("UTC"));
            JSONObject fix=JSON.object("latitude",value.getLatitude(),"longitude",value.getLongitude(),"accuracyMeters",value.getAccuracy(),"time",format.format(new Date(value.getTime())));
            if(value.hasAltitude())fix.put("altitude",value.getAltitude());
            if(value.hasSpeed())fix.put("speed",value.getSpeed());
            if(value.hasBearing())fix.put("bearing",value.getBearing());
            JSONObject result=state();result.put("fix",fix);
            if(!result.optBoolean("servicesEnabled")){finish(token,Services.outcome("failed","Location services are off."));return;}
            finish(token,JSON.object("status","completed","location",result));
        }catch(Exception error){finish(token,Services.outcome("failed","The location fix was invalid."));}
    }

    private static boolean finite(double value){return !Double.isNaN(value) && !Double.isInfinite(value);}
    void cancel(){finish(generation,Services.outcome("cancelled","Location read cancelled."));}
    private void finish(long token,JSONObject result) {
        if(token!=generation || completion==null)return;
        Consumer<JSONObject> callback=completion;completion=null;
        if(timeout!=null){host.main.removeCallbacks(timeout);timeout=null;}
        if(cancellation!=null){cancellation.cancel();cancellation=null;}
        if(listener!=null){manager.removeUpdates(listener);listener=null;}
        callback.accept(result);
    }
}
