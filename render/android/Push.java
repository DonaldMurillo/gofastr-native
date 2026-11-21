package dev.gofastr.nativeui;

import android.app.*;
import android.content.*;
import android.net.Uri;
import android.os.*;
import java.security.MessageDigest;
import java.util.*;
import java.util.function.Consumer;
import org.json.JSONArray;
import org.json.JSONObject;

/**
 * Remote push inbox. The push provider (Firebase Cloud Messaging) is compiled
 * in only when the build enables push; without one push/v1 is unavailable.
 * Received messages stay in app-private storage until Go acknowledges them.
 */
public final class Push {
    public interface Callback { void done(String token, Exception error); }
    public interface Provider { void token(Context context, Callback callback); void delete(Context context, Callback callback); }

    private static final String CHANNEL="gofastr.push", STORE="gofastr.push", TOKEN="token", INBOX="inbox";
    /** The data key naming a tap's local route, the same key APNs payloads use on iOS. */
    static final String ROUTE="gofastr.route";
    private static final int MAX_INBOX=32, MAX_DATA=4096;
    private static volatile Provider provider;
    private Push() {}

    public static void install(Provider value) { provider=value; }
    static boolean available() { return provider!=null; }
    private static SharedPreferences store(Context context) { return context.getSharedPreferences(STORE,Context.MODE_PRIVATE); }

    static void perform(Context context, JSONObject request, boolean notificationsAllowed, Consumer<JSONObject> done) {
        Provider current=provider;
        if(current==null){done.accept(JSON.object("status","unsupported","message","Push notifications are not enabled for this build."));return;}
        Handler main=new Handler(Looper.getMainLooper());
        String permission=notificationsAllowed?"authorized":"denied";
        try {
            switch(request.optString("operation")) {
                case "register":
                    current.token(context,(token,error)->main.post(()->{
                        if(error!=null || token==null || token.isEmpty()){done.accept(JSON.object("status","failed","message","Push registration failed. Check the network and Google Play services."));return;}
                        store(context).edit().putString(TOKEN,token).commit();
                        done.accept(JSON.object("status","completed","push",state(permission,true,token),"message","Registered for push notifications."));
                    }));
                    return;
                case "unregister":
                    current.delete(context,(token,error)->main.post(()->{
                        if(error!=null){done.accept(JSON.object("status","failed","message","Push unregistration failed."));return;}
                        store(context).edit().remove(TOKEN).commit();
                        done.accept(JSON.object("status","completed","push",state(permission,false,""),"message","Unregistered from push notifications."));
                    }));
                    return;
                case "status": {
                    String token=store(context).getString(TOKEN,"");
                    done.accept(JSON.object("status","completed","push",state(permission,!token.isEmpty(),token),"message",token.isEmpty()?"Not registered for push notifications.":"Registered for push notifications."));
                    return;
                }
                case "next": {
                    JSONObject result=state(permission,registered(context),"");
                    synchronized(Push.class){JSONArray inbox=inbox(context);if(inbox.length()>0)result.put("message",inbox.getJSONObject(0));}
                    done.accept(JSON.object("status","completed","push",result,"message",result.has("message")?"A push message is waiting.":"No push messages are waiting."));
                    return;
                }
                case "acknowledge": {
                    String id=request.getString("identifier");
                    synchronized(Push.class){
                        JSONArray inbox=inbox(context),kept=new JSONArray();
                        for(int i=0;i<inbox.length();i++)if(!id.equals(inbox.getJSONObject(i).optString("identifier")))kept.put(inbox.getJSONObject(i));
                        if(!store(context).edit().putString(INBOX,kept.toString()).commit())throw new IllegalStateException("Cannot update the push inbox");
                    }
                    ((NotificationManager)context.getSystemService(Context.NOTIFICATION_SERVICE)).cancel(id,1);
                    JSONObject result=state(permission,registered(context),"");
                    done.accept(JSON.object("status","completed","push",result,"message","Push message acknowledged."));
                    return;
                }
                default: done.accept(JSON.object("status","failed","message","Unsupported push operation."));
            }
        } catch(Exception e) { done.accept(JSON.object("status","failed","message","The push inbox could not be read.")); }
    }

    private static boolean registered(Context context) { return !store(context).getString(TOKEN,"").isEmpty(); }

    /** The shared push/v1 state; a token, when present, is always an FCM token. */
    private static JSONObject state(String permission, boolean registered, String token) {
        JSONObject state=JSON.object("permission",permission,"registered",registered,"provider","fcm");
        return token.isEmpty()?state:JSON.object("permission",permission,"registered",registered,"provider","fcm","token",token);
    }

    private static JSONArray inbox(Context context) throws Exception { return new JSONArray(store(context).getString(INBOX,"[]")); }

    /** Records a newly issued token so status reports it before the next register. */
    public static void tokenChanged(Context context, String token) { if(token!=null && !token.isEmpty())store(context).edit().putString(TOKEN,token).apply(); }

    /**
     * A local route Go's push.ValidRoute also accepts. Go rejects a whole
     * inbox entry it cannot parse, and an entry nobody can read is never
     * acknowledged, so control characters and broken escapes drop the route.
     */
    static String localRoute(String route) {
        if(route==null || !route.startsWith("/") || route.startsWith("//") || route.contains("\\") || route.contains("#") || utf8(route)>4096 || !wellFormed(route).equals(route)) return "";
        for(int i=0;i<route.length();i++){
            char c=route.charAt(i);
            if(c<0x20 || c==0x7f) return "";
            if(c=='%' && (i+2>=route.length() || !hex(route.charAt(i+1)) || !hex(route.charAt(i+2)))) return "";
        }
        return route;
    }

    private static boolean hex(char c) { return c>='0' && c<='9' || c>='a' && c<='f' || c>='A' && c<='F'; }

    private static String identifier(String messageId) throws Exception {
        if(messageId==null || messageId.isEmpty())messageId=UUID.randomUUID().toString();
        byte[] digest=MessageDigest.getInstance("SHA-256").digest(messageId.getBytes("UTF-8"));
        StringBuilder out=new StringBuilder();for(int i=0;i<16;i++)out.append(String.format(Locale.ROOT,"%02x",digest[i]));
        return out.toString();
    }

    private static boolean reserved(String key) { return key.startsWith("google.") || key.startsWith("gcm.") || key.equals("from") || key.equals("collapse_key") || key.equals("title") || key.equals("body") || key.equals(ROUTE); }

    /**
     * Stores a received message and, when it has text, shows it. A tap opens
     * the message's local route through the same guard as deep links.
     */
    public static String deliver(Context context, String messageId, String title, String body, Map<String,String> data, boolean notify) {
        try {
            String id=identifier(messageId);
            JSONObject message=new JSONObject();message.put("identifier",id);
            title=plain(bounded(title,200));body=bounded(body,MAX_DATA);
            if(!title.isEmpty())message.put("title",title);
            if(!body.isEmpty())message.put("body",body);
            String route=localRoute(data==null?null:data.get(ROUTE));if(!route.isEmpty())message.put("route",route);
            JSONObject values=new JSONObject();int total=0;
            if(data!=null)for(Map.Entry<String,String> entry:new TreeMap<>(data).entrySet()){
                String key=entry.getKey(),value=entry.getValue()==null?"":entry.getValue();
                int size=key==null?0:utf8(key)+utf8(value);
                if(key==null || key.isEmpty() || utf8(key)>128 || reserved(key) || values.length()>=32 || !plain(key).equals(key) || value.indexOf('\0')>=0 || !wellFormed(key).equals(key) || !wellFormed(value).equals(value))continue;
                if(total+size>MAX_DATA)break;
                total+=size;values.put(key,value);
            }
            if(values.length()>0)message.put("data",values);
            synchronized(Push.class){
                JSONArray inbox=inbox(context),kept=new JSONArray();
                for(int i=0;i<inbox.length();i++)if(!id.equals(inbox.getJSONObject(i).optString("identifier")))kept.put(inbox.getJSONObject(i));
                kept.put(message);
                while(kept.length()>MAX_INBOX)kept.remove(0);
                if(!store(context).edit().putString(INBOX,kept.toString()).commit())throw new IllegalStateException("Cannot store push message");
            }
            if(notify && !(title.isEmpty() && body.isEmpty()))show(context,id,title,body,route);
            return id;
        } catch(Exception e) { android.util.Log.e("gofastr-native","push delivery failed",e); return ""; }
    }

    private static int utf8(String value) { return value.getBytes(java.nio.charset.StandardCharsets.UTF_8).length; }

    private static String plain(String value) {
        StringBuilder out=new StringBuilder(value.length());
        for(int i=0;i<value.length();i++){char c=value.charAt(i);out.append(Character.isISOControl(c)?' ':c);}
        return out.toString();
    }

    /**
     * At most maximum UTF-8 bytes, cut at a code point. Lone surrogates
     * become U+FFFD first: Go receives them as three-byte U+FFFD, so a Java
     * count of them, or a cut through a surrogate pair, could exceed Go's bound.
     */
    private static String bounded(String value, int maximum) {
        if(value==null)return "";
        value=wellFormed(value.replace("\0",""));
        int bytes=0,end=0;
        while(end<value.length()){
            int point=value.codePointAt(end),size=point<0x80?1:point<0x800?2:point<0x10000?3:4;
            if(bytes+size>maximum)break;
            bytes+=size;end+=Character.charCount(point);
        }
        return value.substring(0,end);
    }

    private static String wellFormed(String value) {
        StringBuilder out=null;
        for(int i=0;i<value.length();i++){
            char c=value.charAt(i);
            boolean pair=Character.isHighSurrogate(c) && i+1<value.length() && Character.isLowSurrogate(value.charAt(i+1));
            if(pair){if(out!=null)out.append(c).append(value.charAt(i+1));i++;continue;}
            if(Character.isSurrogate(c)){if(out==null)out=new StringBuilder(value.substring(0,i));out.append('\uFFFD');}
            else if(out!=null)out.append(c);
        }
        return out==null?value:out.toString();
    }

    private static void show(Context context, String id, String title, String body, String route) {
        NotificationManager manager=(NotificationManager)context.getSystemService(Context.NOTIFICATION_SERVICE);
        if(!manager.areNotificationsEnabled())return;
        if(Build.VERSION.SDK_INT>=26)manager.createNotificationChannel(new NotificationChannel(CHANNEL,"Push notifications",NotificationManager.IMPORTANCE_DEFAULT));
        Intent launch=context.getPackageManager().getLaunchIntentForPackage(context.getPackageName());if(launch==null)return;
        launch.setFlags(Intent.FLAG_ACTIVITY_NEW_TASK|Intent.FLAG_ACTIVITY_SINGLE_TOP|Intent.FLAG_ACTIVITY_CLEAR_TOP).setData(new Uri.Builder().scheme("gofastr-push").authority(context.getPackageName()).appendPath(id).build()).putExtra("gofastr.push",true).putExtra("route",route);
        PendingIntent tap=PendingIntent.getActivity(context,0,launch,PendingIntent.FLAG_UPDATE_CURRENT|PendingIntent.FLAG_IMMUTABLE);
        Notification.Builder builder=Build.VERSION.SDK_INT>=26?new Notification.Builder(context,CHANNEL):new Notification.Builder(context);
        builder.setSmallIcon(android.R.drawable.stat_notify_chat).setContentTitle(title).setContentText(body).setStyle(new Notification.BigTextStyle().bigText(body)).setAutoCancel(true).setContentIntent(tap);
        manager.notify(id,1,builder.build());
    }
}
