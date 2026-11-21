package dev.gofastr.nativeui;

import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.os.BatteryManager;
import android.os.Build;
import android.os.PowerManager;
import java.util.Locale;
import org.json.JSONObject;

final class DeviceInfo {
    static JSONObject snapshot(Context context, JSONObject request) throws Exception {
        if(request.length()!=1)throw new IllegalArgumentException("Device snapshot has no arguments");
        String operation=request.getString("operation");
        JSONObject result;
        if(operation.equals("info")) {
            String version=context.getPackageManager().getPackageInfo(context.getPackageName(),0).versionName;
            result=JSON.object("info",JSON.object("platform","android","manufacturer",Build.MANUFACTURER,
                "model",Build.MODEL,"osVersion",Build.VERSION.RELEASE,"apiLevel",Build.VERSION.SDK_INT,
                "locale",Locale.getDefault().toLanguageTag(),"appVersion",version==null?"":version));
        }else if(operation.equals("battery")) {
            Intent intent=context.registerReceiver(null,new IntentFilter(Intent.ACTION_BATTERY_CHANGED));
            int level=intent==null?-1:intent.getIntExtra(BatteryManager.EXTRA_LEVEL,-1);
            int scale=intent==null?-1:intent.getIntExtra(BatteryManager.EXTRA_SCALE,-1);
            boolean known=level>=0 && scale>0 && level<=scale;
            String state="unknown",source="unknown";
            if(intent!=null) {
                switch(intent.getIntExtra(BatteryManager.EXTRA_STATUS,-1)) {
                    case BatteryManager.BATTERY_STATUS_CHARGING: state="charging";break;
                    case BatteryManager.BATTERY_STATUS_DISCHARGING: state="discharging";break;
                    case BatteryManager.BATTERY_STATUS_FULL: state="full";break;
                    case BatteryManager.BATTERY_STATUS_NOT_CHARGING: state="not_charging";break;
                    default: break;
                }
                switch(intent.getIntExtra(BatteryManager.EXTRA_PLUGGED,-1)) {
                    case 0: source="none";break;
                    case BatteryManager.BATTERY_PLUGGED_AC: source="ac";break;
                    case BatteryManager.BATTERY_PLUGGED_USB: source="usb";break;
                    case BatteryManager.BATTERY_PLUGGED_WIRELESS: source="wireless";break;
                    default: break;
                }
            }
            PowerManager power=(PowerManager)context.getSystemService(Context.POWER_SERVICE);
            result=JSON.object("battery",JSON.object("levelKnown",known,"level",known?(int)(level*100L/scale):0,
                "state",state,"powerSource",source,"powerSave",power!=null && power.isPowerSaveMode()));
        }else throw new IllegalArgumentException("Unknown device snapshot");
        return JSON.object("status","completed","deviceInfo",result);
    }
}
