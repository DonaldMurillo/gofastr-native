package dev.gofastr.nativeui.fcm;

import android.content.Context;
import com.google.firebase.messaging.FirebaseMessaging;
import dev.gofastr.nativeui.Push;

/** Firebase Cloud Messaging provider, compiled only into push-enabled builds. */
public final class FcmPush implements Push.Provider {
    public static void install() { Push.install(new FcmPush()); }

    @Override public void token(Context context, Push.Callback callback) {
        FirebaseMessaging.getInstance().getToken().addOnCompleteListener(task -> {
            if(task.isSuccessful())callback.done(task.getResult(),null);
            else callback.done(null,task.getException()==null?new IllegalStateException("No token"):task.getException());
        });
    }

    @Override public void delete(Context context, Push.Callback callback) {
        FirebaseMessaging.getInstance().deleteToken().addOnCompleteListener(task -> callback.done(null,task.isSuccessful()?null:(task.getException()==null?new IllegalStateException("Not deleted"):task.getException())));
    }
}
