package dev.gofastr.nativeui;

import android.app.ActivityManager;
import android.app.job.JobInfo;
import android.app.job.JobParameters;
import android.app.job.JobScheduler;
import android.app.job.JobService;
import android.content.ComponentName;
import android.content.Context;
import android.os.Build;
import android.os.Handler;
import android.os.SystemClock;
import org.json.JSONObject;
import java.util.HashMap;
import java.util.Map;

/** The `refresh` service as one persisted periodic job; the OS picks run times. */
public abstract class BackgroundJobs extends JobService {
    protected abstract GofastrActivity.Backend createBackend();
    /** Calls the bound package's BackgroundRefresh(timeoutMillis). */
    protected abstract void runRefresh(long timeoutMillis) throws Exception;
    /** Calls the bound package's CancelBackgroundRefresh(). */
    protected abstract void cancelRefresh();

    /** Matches the iOS run budget; device/refresh caps runs at 25 seconds either way. */
    static final long RUN_MILLIS = 25_000;
    static final int MIN_MINUTES = 15, MAX_MINUTES = 7 * 24 * 60;
    /** One app-wide job. The ID is fixed so schedule replaces rather than adds. */
    static final int JOB_ID = 0x67667266;
    private final Map<Integer, Run> active = new HashMap<>();
    private static final class Run { volatile boolean stopped; boolean done; }
    private static JobScheduler scheduler(Context context) { return (JobScheduler)context.getSystemService(Context.JOB_SCHEDULER_SERVICE); }
    private static ComponentName component(Context context) { return new ComponentName(context.getPackageName(), context.getPackageName()+".BackgroundService"); }

    static synchronized JSONObject perform(Context context, JSONObject request) {
        try {
            String op = request.getString("operation");
            long interval = request.optLong("intervalMinutes", 0);
            JobScheduler scheduler = scheduler(context);
            JobInfo existing = scheduler.getPendingJob(JOB_ID);
            // Never replace or cancel a job some other code registered under this ID.
            if (existing != null && !component(context).equals(existing.getService())) throw new IllegalStateException();
            boolean scheduled = existing != null;
            if ("schedule".equals(op)) {
                if (interval < MIN_MINUTES || interval > MAX_MINUTES) throw new IllegalArgumentException();
                if (Build.VERSION.SDK_INT >= 28 && ((ActivityManager)context.getSystemService(Context.ACTIVITY_SERVICE)).isBackgroundRestricted())
                    return JSON.object("status","denied","permission","restricted","message","Background activity is restricted for this app in battery settings.");
                JobInfo job = new JobInfo.Builder(JOB_ID, component(context)).setPersisted(true).setPeriodic(interval*60_000L).build();
                if (scheduler.schedule(job) != JobScheduler.RESULT_SUCCESS) throw new IllegalStateException();
                scheduled = true;
            } else if ("cancel".equals(op)) {
                if (interval != 0) throw new IllegalArgumentException();
                scheduler.cancel(JOB_ID);
                scheduled = false;
            } else if (!"status".equals(op) || interval != 0) throw new IllegalArgumentException();
            return JSON.object("status","completed","refresh",JSON.object("scheduled",scheduled));
        } catch (Exception error) { return JSON.object("status","failed","message","The background refresh could not be updated."); }
    }

    @Override public boolean onStartJob(JobParameters parameters) {
        Run run = new Run();
        synchronized (active) { active.put(parameters.getJobId(), run); }
        new Thread(() -> {
            GofastrActivity.Backend backend = null;
            try {
                backend = createBackend();
                BackendRuntime.acquire(backend, getFilesDir().getAbsolutePath()+"/app");
            } catch (Exception error) { backend = null; }
            try { if (backend != null && !run.stopped) runRefresh(RUN_MILLIS); }
            catch (Exception ignored) {}
            finally {
                synchronized (run) { run.done = true; }
                if (backend != null) try { BackendRuntime.release(backend); } catch (Exception ignored) {}
            }
            new Handler(getMainLooper()).post(() -> {
                synchronized (active) { if (active.get(parameters.getJobId()) == run) active.remove(parameters.getJobId()); }
                // A periodic job runs again next period; a failed run does not retry early.
                if (!run.stopped) jobFinished(parameters, false);
            });
        }, "gofastr-background-refresh").start();
        return true;
    }

    @Override public boolean onStopJob(JobParameters parameters) {
        final Run run;
        synchronized (active) { run = active.remove(parameters.getJobId()); }
        if (run != null) {
            run.stopped = true;
            new Thread(() -> stop(run), "gofastr-background-refresh-stop").start();
        }
        return false;
    }

    /**
     * Go ignores a cancel that arrives before Run registers its context, so a
     * stop between the worker's stopped check and that registration would be
     * lost. Keep cancelling until this run's worker has returned.
     */
    private void stop(Run run) {
        long deadline = SystemClock.uptimeMillis() + RUN_MILLIS;
        while (SystemClock.uptimeMillis() < deadline) {
            synchronized (run) {
                if (run.done) return;
                cancelRefresh();
            }
            try { Thread.sleep(50); } catch (InterruptedException interrupted) { return; }
        }
    }
}
