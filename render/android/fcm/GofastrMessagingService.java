package dev.gofastr.nativeui.fcm;

import com.google.firebase.messaging.FirebaseMessagingService;
import com.google.firebase.messaging.RemoteMessage;
import dev.gofastr.nativeui.Push;
import java.util.Map;

/**
 * Receives data messages, and notification messages while the app is in the
 * foreground. The system shows background notification messages itself; their
 * data reaches the inbox when the user opens them.
 */
public final class GofastrMessagingService extends FirebaseMessagingService {
    @Override public void onMessageReceived(RemoteMessage message) {
        Map<String,String> data=message.getData();
        RemoteMessage.Notification notification=message.getNotification();
        String title=notification!=null && notification.getTitle()!=null?notification.getTitle():data.get("title");
        String body=notification!=null && notification.getBody()!=null?notification.getBody():data.get("body");
        Push.deliver(this,message.getMessageId(),title,body,data,true);
    }

    @Override public void onNewToken(String token) { Push.tokenChanged(this,token); }
}
