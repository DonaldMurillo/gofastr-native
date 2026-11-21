package dev.gofastr.nativeui;

/** Process-local leases shared by activities and scheduler jobs. */
final class BackendRuntime {
    private static int leases;
    static synchronized void acquire(GofastrActivity.Backend backend, String directory) throws Exception {
        if (leases == 0) backend.start(directory);
        leases++;
    }
    static synchronized void release(GofastrActivity.Backend backend) throws Exception {
        if (leases > 0 && --leases == 0) backend.stop();
    }
}
