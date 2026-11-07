package mobilehost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localapp"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type lifecycleBattery struct {
	init  func(*framework.App) error
	start func(context.Context) error
	stop  func(context.Context) error
}

func (*lifecycleBattery) Name() string { return "lifecycle-test" }
func (battery *lifecycleBattery) Init(app *framework.App) error {
	if battery.init != nil {
		return battery.init(app)
	}
	return nil
}
func (battery *lifecycleBattery) OnStart(ctx context.Context) error {
	if battery.start != nil {
		return battery.start(ctx)
	}
	return nil
}
func (battery *lifecycleBattery) OnStop(ctx context.Context) error {
	if battery.stop != nil {
		return battery.stop(ctx)
	}
	return nil
}

func lifecycleHost(t *testing.T, configure func(*framework.App), timeout time.Duration) (*framework.App, *localapp.Data, *Host) {
	t.Helper()
	return lifecycleHostMode(t, configure, timeout, true)
}

func lifecycleHostMode(t *testing.T, configure func(*framework.App), timeout time.Duration, serviceOnly bool) (*framework.App, *localapp.Data, *Host) {
	t.Helper()
	t.Cleanup(func() { owner.SetExtractor(nil) })
	data, err := localapp.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	app := framework.NewApp(Options(data)...)
	app.OnStop(data.Close)
	configure(app)
	if !serviceOnly {
		app.Mount(uihost.New(appui.NewApp("lifecycle-test")))
	}
	host, err := New(app, Config{Data: data, ServiceOnly: serviceOnly, StartTimeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = host.Stop(ctx)
	})
	return app, data, host
}

func TestServiceOnlyRunsCompleteLifecycleWithoutUI(t *testing.T) {
	var initialized, started, stopped, seeded atomic.Int32
	app, data, host := lifecycleHost(t, func(app *framework.App) {
		app.Entity("early_items", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
		app.WithSeed(func(ctx context.Context) error {
			seeded.Add(1)
			_, err := app.DB.ExecContext(ctx, "INSERT INTO early_items (id, title) VALUES ('seed', 'ready')")
			return err
		})
		app.RegisterBattery(&lifecycleBattery{
			init: func(app *framework.App) error {
				initialized.Add(1)
				app.Entity("late_items", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
				return nil
			},
			start: func(ctx context.Context) error {
				started.Add(1)
				_, err := app.DB.ExecContext(ctx, "INSERT INTO late_items (id, title) VALUES ('start', 'ready')")
				return err
			},
			stop: func(ctx context.Context) error {
				stopped.Add(1)
				return app.DB.PingContext(ctx)
			},
		})
		app.Router().Get("/identity", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			user, ok := handler.GetUser(request.Context())
			if !ok {
				response.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = response.Write([]byte(user.(*localapp.User).GetID()))
		}))
	}, 0)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	if len(app.Mountables()) != 0 {
		t.Fatal("service-only startup mounted a presentation")
	}
	response := httptest.NewRecorder()
	host.identityMiddleware(app.Router()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/identity", nil))
	if response.Code != http.StatusOK || response.Body.String() != data.User.GetID() {
		t.Fatalf("installation identity = %d %s", response.Code, response.Body.String())
	}
	for _, route := range []string{"/", "/notes", bridgeScriptPath} {
		response := httptest.NewRecorder()
		app.Router().ServeHTTP(response, httptest.NewRequest(http.MethodGet, route, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("unused UI route %s = %d", route, response.Code)
		}
	}
	if initialized.Load() != 1 || started.Load() != 1 || seeded.Load() != 1 {
		t.Fatalf("startup counts: init=%d start=%d seed=%d", initialized.Load(), started.Load(), seeded.Load())
	}
	for range 2 {
		if err := host.Stop(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if stopped.Load() != 1 {
		t.Fatalf("battery stopped %d times", stopped.Load())
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("data survived the app's stop hook")
	}
	if err := host.Start(); err == nil {
		t.Fatal("a stopped host restarted its one-shot App")
	}
}

func requireNoServiceTransport(t *testing.T, host *Host) {
	t.Helper()
	if host.ln != nil || host.client != nil || host.jar != nil {
		t.Fatal("service-only host constructed a listener, HTTP client or cookie jar")
	}
}

func TestServiceOnlyHasNoTransport(t *testing.T) {
	var wrapped atomic.Int32
	app, _, host := lifecycleHost(t, func(app *framework.App) {
		app.WrapServer(func(next http.Handler) http.Handler {
			wrapped.Add(1)
			return next
		})
	}, 0)
	t.Run("construction", func(t *testing.T) { requireNoServiceTransport(t, host) })
	assertUnavailable := func(t *testing.T) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		for _, request := range []*Request{nil, {URL: DefaultOrigin + "/"}} {
			if response, err := host.Do(ctx, request); response != nil || !errors.Is(err, ErrServiceOnly) {
				t.Errorf("Do = %+v, %v; want ErrServiceOnly", response, err)
			}
			if stream, err := host.Open(ctx, request); stream != nil || !errors.Is(err, ErrServiceOnly) {
				if stream != nil {
					_ = stream.Close()
				}
				t.Errorf("Open = %+v, %v; want ErrServiceOnly", stream, err)
			}
		}
	}
	t.Run("before-start", assertUnavailable)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-app.ServicesReady():
	default:
		t.Fatal("Start returned before ServicesReady")
	}
	if wrapped.Load() != 0 {
		t.Fatal("service-only startup composed the HTTP server handler")
	}
	t.Run("running", assertUnavailable)
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Run("after-stop", assertUnavailable)
	requireNoServiceTransport(t, host)
}

func TestServiceOnlyStopDuringBoot(t *testing.T) {
	for _, phase := range []string{"seed", "start"} {
		t.Run(phase, func(t *testing.T) {
			entered := make(chan struct{})
			finished := make(chan struct{})
			var stopped atomic.Int32
			app, data, host := lifecycleHost(t, func(app *framework.App) {
				block := func(ctx context.Context) error {
					close(entered)
					defer close(finished)
					<-ctx.Done()
					return ctx.Err()
				}
				if phase == "seed" {
					app.WithSeed(block)
				} else {
					app.OnStart(block)
				}
				app.OnStop(func() error { stopped.Add(1); return nil })
			}, 0)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			startDone := make(chan error, 1)
			go func() { startDone <- host.Start() }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("boot hook did not start")
			}
			if err := host.Stop(ctx); err != nil {
				t.Fatalf("Stop during %s = %v", phase, err)
			}
			select {
			case err := <-startDone:
				if err == nil {
					t.Fatal("Start reported readiness after boot was stopped")
				}
			case <-ctx.Done():
				t.Fatal("Stop left Start waiting")
			}
			select {
			case <-finished:
			default:
				t.Fatal("Stop left the boot hook running")
			}
			select {
			case <-host.done:
			default:
				t.Fatal("Stop left the app run goroutine running")
			}
			select {
			case <-app.ServicesReady():
				t.Fatal("stopped boot published readiness")
			default:
			}
			if stopped.Load() != 1 || data.DB.Ping() == nil {
				t.Fatalf("boot teardown: stops=%d, database must be closed", stopped.Load())
			}
			requireNoServiceTransport(t, host)
		})
	}
}

func TestServiceOnlyConcurrentStartStop(t *testing.T) {
	entered := make(chan struct{})
	failure := errors.New("stop hook failed")
	var stopped atomic.Int32
	_, _, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStart(func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		app.OnStop(func() error { stopped.Add(1); return failure })
	}, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	startDone := make(chan error, 1)
	go func() { startDone <- host.Start() }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("boot hook did not start")
	}
	if err := host.Start(); err == nil {
		t.Fatal("concurrent Start admitted a second run")
	}
	stops := make(chan error, 8)
	for range cap(stops) {
		go func() { stops <- host.Stop(ctx) }()
	}
	var first error
	for range cap(stops) {
		select {
		case err := <-stops:
			if !errors.Is(err, failure) {
				t.Fatalf("concurrent Stop lost teardown error: %v", err)
			}
			if first == nil {
				first = err
			} else if err != first {
				t.Fatalf("concurrent Stops returned different results: %v, %v", first, err)
			}
		case <-ctx.Done():
			t.Fatal("concurrent Stop leaked a caller")
		}
	}
	select {
	case err := <-startDone:
		if err == nil {
			t.Fatal("stopped boot reported success")
		}
	case <-ctx.Done():
		t.Fatal("concurrent Stop left Start running")
	}
	if stopped.Load() != 1 {
		t.Fatalf("concurrent Stop ran teardown %d times", stopped.Load())
	}
	requireNoServiceTransport(t, host)
}

func TestServiceOnlyRejectsReadyHooksDuringBoot(t *testing.T) {
	var stopped atomic.Int32
	_, data, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStart(func(context.Context) error {
			app.OnReady(func(string) {})
			return nil
		})
		app.OnStop(func() error { stopped.Add(1); return nil })
	}, 0)
	if err := host.Start(); err == nil || !strings.Contains(err.Error(), "OnReady") {
		t.Fatalf("listener-dependent startup = %v; want OnReady refusal", err)
	}
	for range 2 {
		if err := host.Stop(context.Background()); err == nil || !strings.Contains(err.Error(), "OnReady") {
			t.Fatalf("Stop lost startup failure: %v", err)
		}
	}
	if stopped.Load() != 1 || data.DB.Ping() == nil {
		t.Fatalf("startup rollback: stops=%d, database must be closed", stopped.Load())
	}
}

func TestServiceOnlyStopWaitsForTeardown(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	failure := errors.New("teardown failed")
	_, _, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStop(func() error {
			close(entered)
			<-release
			return failure
		})
	}, 0)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopDone := make(chan error, 1)
	go func() { stopDone <- host.Stop(ctx) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("teardown did not start")
	}
	select {
	case <-host.done:
	case <-ctx.Done():
		t.Fatal("app run did not return while teardown was pending")
	}
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before teardown finished: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-stopDone:
		if !errors.Is(err, failure) {
			t.Fatalf("Stop lost teardown failure: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Stop did not join teardown")
	}
	requireNoServiceTransport(t, host)
}

func TestStartupFailureDrainsAndStopIsRepeatable(t *testing.T) {
	failure := errors.New("startup failed")
	for _, phase := range []string{"early-validation", "http-validation", "migration", "seed", "init", "start"} {
		t.Run(phase, func(t *testing.T) {
			var stopped atomic.Int32
			_, data, host := lifecycleHostMode(t, func(app *framework.App) {
				app.OnStop(func() error { stopped.Add(1); return nil })
				switch phase {
				case "early-validation":
					app.OnReady(func(string) {})
				case "http-validation":
					app.Config.RequestTimeout = time.Minute
					app.Config.HTTPServerTimeouts.WriteTimeout = new(time.Second)
				case "migration":
					app.Entity("broken", framework.EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
					_ = app.DB.Close()
				case "seed":
					app.WithSeed(func(context.Context) error { return failure })
				case "init":
					app.RegisterBattery(&lifecycleBattery{init: func(*framework.App) error { return failure }})
				case "start":
					app.OnStart(func(context.Context) error { return failure })
				}
			}, 0, phase != "http-validation")
			if err := host.Start(); err == nil {
				t.Fatal("startup failure was hidden")
			} else if phase != "migration" && phase != "early-validation" && phase != "http-validation" && !errors.Is(err, failure) {
				t.Fatalf("lost startup cause: %v", err)
			}
			if stopped.Load() != 1 {
				t.Fatalf("failed startup ran %d stop hooks", stopped.Load())
			}
			if err := data.DB.Ping(); err == nil {
				t.Fatal("failed startup leaked the database")
			}
			for range 2 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				err := host.Stop(ctx)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Stop after failed Start consumed the completion: %v", err)
				}
			}
			if stopped.Load() != 1 {
				t.Fatal("failed startup teardown was replayed")
			}
		})
	}
}

func TestStartupTimeoutCancelsAndJoinsWorkers(t *testing.T) {
	workerStarted := make(chan struct{})
	workerDone := make(chan struct{})
	_, data, host := lifecycleHost(t, func(app *framework.App) {
		app.OnStart(func(ctx context.Context) error {
			close(workerStarted)
			defer close(workerDone)
			<-ctx.Done()
			return ctx.Err()
		})
	}, 100*time.Millisecond)
	if err := host.Start(); err == nil || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("startup timeout = %v", err)
	}
	select {
	case <-host.done:
	default:
		t.Fatal("startup returned before the app run finished")
	}
	// The deadline also covers migrations. Under load it can expire before
	// OnStart runs, so only a worker that actually started must be joined.
	select {
	case <-workerStarted:
		select {
		case <-workerDone:
		default:
			t.Fatal("startup returned before its worker was cancelled")
		}
	default:
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("startup timeout leaked the database")
	}
}

func TestServiceOnlyPreservesForeignAuthenticator(t *testing.T) {
	app, _, host := lifecycleHost(t, func(app *framework.App) {
		owner.SetExtractor(func(context.Context) (any, bool) { return "authenticated", true })
		app.Router().Get("/identity", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if _, ok := handler.GetUser(request.Context()); ok {
				response.WriteHeader(http.StatusForbidden)
				return
			}
			response.WriteHeader(http.StatusNoContent)
		}))
	}, 0)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	host.identityMiddleware(app.Router()).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/identity", nil))
	if response.Code != http.StatusNoContent {
		t.Fatalf("local identity replaced the authenticator: %d", response.Code)
	}
}

func TestStopBeforeStartClosesComposition(t *testing.T) {
	_, data, host := lifecycleHost(t, func(*framework.App) {}, 0)
	if err := host.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := data.DB.Ping(); err == nil {
		t.Fatal("abandoned composition leaked its database")
	}
	if err := host.Start(); err == nil {
		t.Fatal("Start admitted an already stopped app")
	}
}

func TestStopDrainsLongRequestWithOneShutdownOwner(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var stopped atomic.Int32
	_, _, host := lifecycleHostMode(t, func(app *framework.App) {
		app.OnStop(func() error { stopped.Add(1); return nil })
		app.Router().Get("/held", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			close(entered)
			select {
			case <-release:
				response.WriteHeader(http.StatusNoContent)
			case <-request.Context().Done():
			}
		}))
	}, 0, false)
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	requestDone := make(chan error, 1)
	go func() {
		response, err := host.Do(ctx, &Request{URL: DefaultOrigin + "/held"})
		if err == nil && response.Status != http.StatusNoContent {
			err = errors.New("held request did not complete normally")
		}
		requestDone <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("request did not reach the app")
	}
	stopDone := make(chan error, 3)
	go func() { stopDone <- host.Stop(ctx) }()
	select {
	case <-host.ln.closed:
	case <-ctx.Done():
		t.Fatal("shutdown did not close the listener")
	}
	for range 2 {
		go func() { stopDone <- host.Stop(ctx) }()
	}
	timer := time.NewTimer(5200 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-stopDone:
		t.Fatalf("Stop returned before the request drained: %v", err)
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(release)
	if err := <-requestDone; err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := <-stopDone; err != nil {
			t.Fatalf("Stop reported a follower timeout after the request drained: %v", err)
		}
	}
	if stopped.Load() != 1 {
		t.Fatalf("shutdown ran %d stop hooks", stopped.Load())
	}
	if err := host.Stop(ctx); err != nil {
		t.Fatalf("repeated Stop = %v", err)
	}
}

func TestStopCancelsOutstandingBridgeRequestsBeforeClosingStorage(t *testing.T) {
	entered := make(chan struct{})
	finished := make(chan struct{})
	_, _, host := lifecycleHostMode(t, func(app *framework.App) {
		app.Router().Get("/watch", http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			close(entered)
			<-request.Context().Done()
			close(finished)
		}))
		app.OnStop(func() error {
			select {
			case <-finished:
				return nil
			default:
				return errors.New("bridge handler outlived request drain")
			}
		})
	}, 0, false)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(BridgeRequest{ID: "held-watch", Method: http.MethodGet, URL: DefaultOrigin + "/watch"})
	if err != nil {
		t.Fatal(err)
	}
	replyDone := make(chan string, 1)
	go func() { replyDone <- host.Bridge(string(encoded)) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("bridge request did not reach the app")
	}
	if err := host.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case encodedReply := <-replyDone:
		var reply BridgeReply
		if err := json.Unmarshal([]byte(encodedReply), &reply); err != nil || reply.ID != "held-watch" || !strings.Contains(reply.Error, "context canceled") {
			t.Fatalf("cancelled bridge reply = %+v, %v", reply, err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown left a bridge call waiting")
	}
}
