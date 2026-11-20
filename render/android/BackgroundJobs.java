package dev.gofastr.nativeui;

import android.app.job.JobInfo;
import android.app.job.JobParameters;
import android.app.job.JobScheduler;
import android.app.job.JobService;
import android.content.ComponentName;
import android.content.Context;
import android.os.PersistableBundle;
import org.json.JSONObject;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;

/** Deferrable persisted jobs; the OS controls actual execution times. */
public abstract class BackgroundJobs extends JobService {
    protected abstract GofastrActivity.Backend createBackend();
    private final Map<Integer, Run> active = new HashMap<>();
    private static final class Run {
        final String token = UUID.randomUUID().toString();
        volatile boolean stopped;
        volatile GofastrActivity.Backend backend;
    }
    private static JobScheduler scheduler(Context context) { return (JobScheduler)context.getSystemService(Context.JOB_SCHEDULER_SERVICE); }
    private static ComponentName component(Context context) { return new ComponentName(context.getPackageName(), context.getPackageName()+".BackgroundService"); }
    private static int jobID(String identifier) { return identifier.hashCode() & 0x7fffffff; }

    static synchronized JSONObject perform(Context context, JSONObject request) {
        try {
            String id = request.getString("identifier"), op = request.getString("operation");
            if (!id.matches("[A-Za-z0-9._:-]{1,128}")) throw new IllegalArgumentException();
            JobScheduler scheduler = scheduler(context);
            int key = jobID(id);
            JobInfo existing = scheduler.getPendingJob(key);
            boolean scheduled = existing != null;
            // Hash collisions must never replace or cancel another identifier.
            if (existing != null && (!component(context).equals(existing.getService()) || !id.equals(existing.getExtras().getString("identifier")))) throw new IllegalArgumentException();
            if ("schedule".equals(op)) {
                String task = request.getString("task"), network = request.optString("network", "");
                long delay = request.optLong("delayMinutes",0), interval = request.optLong("intervalMinutes",0);
                boolean periodic = request.optBoolean("periodic",false);
                if (!task.matches("[A-Za-z0-9._:-]{1,128}") || delay < 0 || delay > 525600 || interval < 0 || interval > 525600 || (periodic ? interval < 15 || delay != 0 : interval != 0)) throw new IllegalArgumentException();
                int net;
                if (network.isEmpty()) net = JobInfo.NETWORK_TYPE_NONE;
                else if ("any".equals(network)) net = JobInfo.NETWORK_TYPE_ANY;
                else if ("unmetered".equals(network)) net = JobInfo.NETWORK_TYPE_UNMETERED;
                else throw new IllegalArgumentException();
                PersistableBundle extras = new PersistableBundle();
                extras.putString("identifier",id); extras.putString("task",task);
                JobInfo.Builder builder = new JobInfo.Builder(key,component(context)).setExtras(extras).setPersisted(true).setRequiredNetworkType(net).setRequiresCharging(request.optBoolean("requiresCharging",false));
                if (periodic) builder.setPeriodic(interval*60000L);
                else builder.setMinimumLatency(delay*60000L);
                if (scheduler.schedule(builder.build()) != JobScheduler.RESULT_SUCCESS) throw new IllegalStateException();
                scheduled = true;
            } else if ("cancel".equals(op)) { scheduler.cancel(key); scheduled = false; }
            else if (!"status".equals(op)) throw new IllegalArgumentException();
            return JSON.object("status","completed","background",JSON.object("scheduled",scheduled));
        } catch (Exception error) { return JSON.object("status","failed","message","The background job could not be updated."); }
    }

    @Override public boolean onStartJob(JobParameters parameters) {
        Run run = new Run();
        synchronized (active) { active.put(parameters.getJobId(),run); }
        new Thread(() -> {
            boolean acquired = false, retry = false;
            try {
                run.backend = createBackend();
                BackendRuntime.acquire(run.backend,getFilesDir().getAbsolutePath()+"/app"); acquired = true;
                if (!run.stopped) {
                    JSONObject reply = new JSONObject(run.backend.command(JSON.object("version",1,"kind","background/run","backgroundId",run.token,"backgroundTask",parameters.getExtras().getString("task")).toString()));
                    JSONObject result = reply.optJSONObject("background");
                    retry = result != null && "retry".equals(result.optString("status"));
                }
            } catch (Exception error) { retry = true; }
            finally {
                if (acquired) try { BackendRuntime.release(run.backend); } catch (Exception ignored) {}
            }
            final boolean reschedule = retry;
            new android.os.Handler(getMainLooper()).post(() -> {
                synchronized (active) {
                    if (active.get(parameters.getJobId()) == run) active.remove(parameters.getJobId());
                }
                if (!run.stopped) jobFinished(parameters,reschedule);
            });
        },"gofastr-background").start();
        return true;
    }

    @Override public boolean onStopJob(JobParameters parameters) {
        final Run run;
        synchronized (active) { run = active.remove(parameters.getJobId()); }
        if (run != null) {
            run.stopped = true;
            new Thread(() -> {
                try { if (run.backend != null) run.backend.command(JSON.object("version",1,"kind","background/stop","backgroundId",run.token).toString()); }
                catch (Exception ignored) {}
            },"gofastr-background-stop").start();
        }
        return true;
    }
}
