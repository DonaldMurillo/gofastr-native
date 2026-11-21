package dev.gofastr.nativeui;

import android.content.Context;
import android.net.ConnectivityManager;
import android.net.Network;
import android.net.NetworkCapabilities;
import org.json.JSONObject;

public final class NetworkStatus {
    public static JSONObject perform(Context context, JSONObject request) throws Exception {
        if (request.length()!=1 || !"status".equals(request.optString("operation"))) throw new IllegalArgumentException("Invalid network request");
        ConnectivityManager manager=(ConnectivityManager)context.getSystemService(Context.CONNECTIVITY_SERVICE);
        Network active=manager==null?null:manager.getActiveNetwork();
        NetworkCapabilities capabilities=active==null?null:manager.getNetworkCapabilities(active);
        boolean connected=capabilities!=null;
        String transport="none";
        if(connected) {
            if(capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN))transport="vpn";
            else if(capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI))transport="wifi";
            else if(capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))transport="cellular";
            else if(capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET))transport="ethernet";
            else transport="other";
        }
        return JSON.object("status","completed","network",JSON.object("connected",connected,"transport",transport,
            "metered",connected&&!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),
            "validated",connected&&capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)));
    }
}
