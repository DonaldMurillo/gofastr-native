// Package featuresnative demonstrates one Go application using device adapters
// on both native platforms. Demo secrets are fixed test data, never credentials.
package featuresnative

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr-native/device/authentication"
	"github.com/DonaldMurillo/gofastr-native/device/calendar"
	"github.com/DonaldMurillo/gofastr-native/device/deviceinfo"
	"github.com/DonaldMurillo/gofastr-native/device/links"
	"github.com/DonaldMurillo/gofastr-native/device/network"
	"github.com/DonaldMurillo/gofastr-native/device/push"
	"github.com/DonaldMurillo/gofastr-native/device/refresh"
	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/DonaldMurillo/gofastr-native/device/securestore"
	"github.com/DonaldMurillo/gofastr-native/device/sharein"
	"github.com/DonaldMurillo/gofastr-native/device/widgets"
	"github.com/DonaldMurillo/gofastr-native/extension"
	barcodeext "github.com/DonaldMurillo/gofastr-native/extension/barcode"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

var lifecycle sync.RWMutex
var runtime *nativeui.Runtime
var screens *nativeui.App
var stateDir string

// background records when the OS last ran the demo's refresh. A real app
// would sync its data here instead.
var background = refresh.NewRunner(func(ctx context.Context) error {
	lifecycle.RLock()
	directory := stateDir
	lifecycle.RUnlock()
	if directory == "" {
		return errors.New("featuresnative: not started")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, "last-refresh"), []byte(time.Now().UTC().Format(time.RFC3339)), 0o600)
})

// BackgroundRefresh runs one refresh when iOS or Android wakes the app.
func BackgroundRefresh(timeoutMillis int64) error { return background.Run(timeoutMillis) }

// CancelBackgroundRefresh stops a running refresh when the OS ends the task.
func CancelBackgroundRefresh() { background.Cancel() }

func lastRefresh() string {
	lifecycle.RLock()
	directory := stateDir
	lifecycle.RUnlock()
	stamp, err := os.ReadFile(filepath.Join(directory, "last-refresh"))
	if directory == "" || err != nil {
		return "never"
	}
	if at, err := time.Parse(time.RFC3339, string(stamp)); err == nil {
		return at.Format("2006-01-02 15:04 MST")
	}
	return "never"
}

func Start(dataDir string) error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	if runtime != nil {
		return errors.New("featuresnative: already started")
	}
	app, err := newApp()
	if err != nil {
		return err
	}
	r, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	screens, runtime, stateDir = app, r, dataDir
	return nil
}

func Stop() error {
	lifecycle.Lock()
	defer lifecycle.Unlock()
	screens, runtime, stateDir = nil, nil, ""
	return nil
}
func Root() string { return "/features" }

func Command(payload string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if runtime == nil {
		return "", errors.New("featuresnative: not started")
	}
	return runtime.CommandJSON(context.Background(), payload)
}
func Render(route, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("featuresnative: not started")
	}
	return screens.RenderJSON(context.Background(), route, values)
}
func Dispatch(route, action, values string) (string, error) {
	lifecycle.RLock()
	defer lifecycle.RUnlock()
	if screens == nil {
		return "", errors.New("featuresnative: not started")
	}
	return screens.DispatchJSON(context.Background(), route, action, values)
}

func newApp() (*nativeui.App, error) {
	var inboxMu sync.Mutex
	var next *sharein.Item
	var pushed *push.Message
	var scanMessage string
	service := func(request nativeui.ServiceRequest) nativeui.Handle {
		request.ID = "device"
		return func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &request}, nil
		}
	}
	secure := func(op securestore.Operation, policy securestore.Protection) nativeui.Handle {
		r := securestore.Request{Operation: op, Protection: policy}
		if op == securestore.Put || op == securestore.Get || op == securestore.Delete {
			r.Key = "demo"
		}
		if op == securestore.Put {
			r.Value = []byte("gofastr-demo-secret")
		}
		if policy == securestore.Biometric && op != securestore.Delete {
			r.Reason = "Read or save the demo secret"
		}
		return service(nativeui.ServiceRequest{SecureStorage: &r})
	}
	respond := func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		message := "Device result: " + string(result.Status)
		if result.Status == nativeui.EffectCompleted {
			switch {
			case result.Network != nil:
				message = fmt.Sprintf("Connected: %t; transport: %s; metered: %t; validated: %t.", result.Network.Connected, result.Network.Transport, result.Network.Metered, result.Network.Validated)
			case result.DeviceInfo != nil:
				message = snapshotText(*result.DeviceInfo)
			case result.Scan != nil:
				message = fmt.Sprintf("Scanned %s: %s", result.Scan.Symbology, result.Scan.Value)
			case result.Calendar != nil:
				message = calendarMessage(*result.Calendar)
			case result.SecureStorage != nil:
				if result.SecureStorage.Found {
					message = fmt.Sprintf("Read %d secret bytes. The value stays out of the UI.", len(result.SecureStorage.Value))
				} else if result.SecureStorage.Available {
					message = fmt.Sprintf("Secure storage available. Biometrics available: %t.", result.SecureStorage.BiometricAvailable)
				} else {
					message = "Secure operation completed. No secret bytes returned."
				}
			case result.Widget != nil:
				message = fmt.Sprintf("Widget snapshot published: %t.", result.Widget.Published)
			case result.Refresh != nil:
				message = fmt.Sprintf("Background refresh scheduled: %t.", result.Refresh.Scheduled)
			case result.Push != nil:
				inboxMu.Lock()
				pushed = result.Push.Message
				inboxMu.Unlock()
				switch {
				case result.Push.Token != "":
					message = fmt.Sprintf("%s push token %s (%d bytes). Send it to your server; notifications permission: %s.", result.Push.Provider, preview(result.Push.Token[:min(12, len(result.Push.Token))]), len(result.Push.Token), result.Push.Permission)
				case result.Push.Message != nil:
					message = fmt.Sprintf("Push %s: %s %s (%d data keys, route %q).", result.Push.Message.Identifier, result.Push.Message.Title, preview(result.Push.Message.Body), len(result.Push.Message.Data), result.Push.Message.Route)
				case result.Message != "":
					message = result.Message
				default:
					message = fmt.Sprintf("No push token or message. Notifications permission: %s.", result.Push.Permission)
				}
			default:
				item := result.IncomingShare
				inboxMu.Lock()
				next = item
				inboxMu.Unlock()
				if item != nil {
					message = fmt.Sprintf("Share %s: %s %s (%d files). Read again keeps the same item.", item.Identifier, item.Title, preview(item.Text), len(item.Files))
				} else {
					message = "The share inbox is empty or the share was discarded."
				}
			}
		} else if result.Message != "" {
			message += ". " + result.Message
		}
		return nativeui.Result{Announcement: message}, nil
	}
	deviceRoute := func(path, title, text string, actions []nativeui.Action, handlers map[string]nativeui.Handle) nativeui.Route {
		return nativeui.Route{Path: path, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: title, Text: text, Actions: actions}, nil
		}, Actions: handlers, Services: map[string]nativeui.ServiceRespond{"device": respond}}
	}
	withService := func(route nativeui.Route, id string, respond nativeui.ServiceRespond) nativeui.Route {
		route.Services[id] = respond
		return route
	}
	signIn := func(operation authentication.Operation) nativeui.Handle {
		return func(context.Context, nativeui.Request) (nativeui.Result, error) {
			state := demoToken()
			request := authentication.Request{Operation: operation, Nonce: state}
			if operation == authentication.OAuth {
				redirect := "dev.gofastr.features:/oauth"
				challenge := sha256.Sum256([]byte(demoToken()))
				query := url.Values{"response_type": {"code"}, "client_id": {"demo"}, "redirect_uri": {redirect},
					"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])},
					"code_challenge_method": {"S256"}}
				request = authentication.Request{Operation: operation, State: state, RedirectURI: redirect,
					AuthorizationURL: "https://example.com/?" + query.Encode(), PreferEphemeralSession: true}
			}
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "sign-in", Authentication: &request}}, nil
		}
	}
	scanRespond := func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		message := "Device result: " + string(result.Status)
		if result.Scan != nil {
			message = fmt.Sprintf("Scanned %s: %s", result.Scan.Symbology, result.Scan.Value)
		}
		inboxMu.Lock()
		scanMessage = message
		inboxMu.Unlock()
		return nativeui.Result{Navigation: &nativeui.Navigation{Kind: "push", Route: "/features/scanned"}}, nil
	}
	routes := []nativeui.Route{{Path: Root(), Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		rows := []nativeui.Row{
			{ID: "device", Title: "Device and battery", Destination: "/features/device"},
			{ID: "inbox", Title: "Shared inbox", Destination: "/features/inbox"},
			{ID: "network", Title: "Network status", Destination: "/features/network"},
			{ID: "secure", Title: "Secure storage", Destination: "/features/secure"},
			{ID: "calendar", Title: "Calendar events", Destination: "/features/calendar"},
			{ID: "widgets", Title: "Home-screen widget", Destination: "/features/widgets"},
			{ID: "location", Title: "Location", Destination: "/features/location"},
			{ID: "sign-in", Title: "Native sign-in", Destination: "/features/sign-in"},
			{ID: "refresh", Title: "Background refresh", Destination: "/features/refresh"},
			{ID: "push", Title: "Push notifications", Destination: "/features/push"},
		}
		if goruntime.GOOS == "android" {
			rows = append(rows, nativeui.Row{ID: "links", Title: "Web links", Destination: "/features/links"})
		}
		return nativeui.Screen{Title: "Device features", Prompt: "The same Go screens run on Android and iOS.", Actions: []nativeui.Action{{ID: "scan", Label: "Scan code", Primary: true}}, List: &nativeui.List{Rows: rows}}, nil
	}, Actions: map[string]nativeui.Handle{"scan": service(nativeui.ServiceRequest{Scan: &scanner.Request{Title: "Scan code"}})}, Services: map[string]nativeui.ServiceRespond{"device": scanRespond}},
		nativeui.Route{Path: "/features/scanned", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			inboxMu.Lock()
			defer inboxMu.Unlock()
			return nativeui.Screen{Title: "Scan result", Text: scanMessage}, nil
		}},
		deviceRoute("/features/network", "Network status", "Query the active Android network. Re-query after changing connectivity or resuming the app.", []nativeui.Action{{ID: "status", Label: "Check network status", Primary: true}}, map[string]nativeui.Handle{"status": service(nativeui.ServiceRequest{Network: &network.Request{Operation: network.Status}})}),
		deviceRoute("/features/device", "Device and battery", "Read a device or battery snapshot. No permission or subscription is required on Android. Other renderers report unsupported.", []nativeui.Action{
			{ID: "info", Label: "Device info", Primary: true}, {ID: "battery", Label: "Battery"},
		}, map[string]nativeui.Handle{
			"info":    service(nativeui.ServiceRequest{DeviceInfo: &deviceinfo.Request{Operation: deviceinfo.ReadInfo}}),
			"battery": service(nativeui.ServiceRequest{DeviceInfo: &deviceinfo.Request{Operation: deviceinfo.ReadBattery}}),
		}),
		deviceRoute("/features/inbox", "Shared inbox", "Share text or files from another app, then choose Read next. Read does not consume it. Discard removes only the reviewed share.", []nativeui.Action{
			{ID: "read", Label: "Read next", Primary: true}, {ID: "discard", Label: "Discard reviewed share"},
		}, map[string]nativeui.Handle{
			"read": service(nativeui.ServiceRequest{IncomingShare: &sharein.Request{Operation: sharein.Next}}),
			"discard": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				inboxMu.Lock()
				defer inboxMu.Unlock()
				if next == nil {
					return nativeui.Result{Announcement: "Read a share before discarding it."}, nil
				}
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device", IncomingShare: &sharein.Request{Operation: sharein.Acknowledge, Identifier: next.Identifier}}}, nil
			},
		}),
		deviceRoute("/features/secure", "Secure storage", "These actions use a fixed demo secret. Authentication confirms your presence; it does not encrypt this app's database.", []nativeui.Action{
			{ID: "status", Label: "Check availability", Primary: true}, {ID: "put", Label: "Store demo secret"}, {ID: "get", Label: "Read demo secret"}, {ID: "delete", Label: "Delete demo secret"},
			{ID: "biometric-put", Label: "Store with biometrics"}, {ID: "biometric-get", Label: "Read with biometrics"}, {ID: "biometric-delete", Label: "Delete biometric secret"}, {ID: "authenticate", Label: "Authenticate"},
		}, map[string]nativeui.Handle{
			"status": secure(securestore.Status, ""), "put": secure(securestore.Put, securestore.Device), "get": secure(securestore.Get, securestore.Device), "delete": secure(securestore.Delete, securestore.Device),
			"biometric-put": secure(securestore.Put, securestore.Biometric), "biometric-get": secure(securestore.Get, securestore.Biometric), "biometric-delete": secure(securestore.Delete, securestore.Biometric), "authenticate": secure(securestore.Authenticate, securestore.Biometric),
		}),
		nativeui.Route{Path: "/features/sign-in", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Native sign-in", Text: "This demo tests presentation only. It has no OAuth provider and never signs you in.",
				Actions: []nativeui.Action{{ID: "apple", Label: "Sign in with Apple", Primary: true}, {ID: "oauth", Label: "Open OAuth login"}}}, nil
		}, Actions: map[string]nativeui.Handle{"apple": signIn(authentication.Apple), "oauth": signIn(authentication.OAuth)},
			Services: map[string]nativeui.ServiceRespond{"sign-in": func(_ context.Context, _ nativeui.Request, outcome nativeui.ServiceResult) (nativeui.Result, error) {
				return nativeui.Result{Announcement: "Sign-in result: " + string(outcome.Status) + ". No app account was created."}, nil
			}}},
		withService(deviceRoute("/features/calendar", "Calendar events", "Add a sample event to your default calendar, then read the next seven days. Calendar access is enabled with --calendar on iOS. On Android, Open calendar editor shows the system editor; closing it does not confirm a save.", []nativeui.Action{
			{ID: "calendar-add", Label: "Add sample event", Primary: true},
			{ID: "calendar-status", Label: "Calendar status"},
			{ID: "calendar-read", Label: "Read next 7 days"},
			{ID: "calendar-edit", Label: "Open calendar editor"},
		}, map[string]nativeui.Handle{
			"calendar-edit": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				start := time.Now().Truncate(time.Hour).Add(time.Hour)
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "calendar-editor", Calendar: &calendar.Request{Operation: calendar.Edit, Event: &calendar.Event{Title: "GoFastr sample event", Start: start, End: start.Add(time.Hour)}}}}, nil
			},
			"calendar-status": service(nativeui.ServiceRequest{Calendar: &calendar.Request{Operation: calendar.Status}}),
			"calendar-add": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				start := time.Now().AddDate(0, 0, 1).Truncate(time.Minute)
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device", Calendar: &calendar.Request{Operation: calendar.Add, Event: &calendar.Event{Title: "GoFastr sample event", Start: start, End: start.Add(time.Hour)}}}}, nil
			},
			"calendar-read": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				now := time.Now()
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device", Calendar: &calendar.Request{Operation: calendar.Read, From: now, To: now.AddDate(0, 0, 7), Limit: calendar.MaxLimit}}}, nil
			},
		}), "calendar-editor", calendarEditorRespond),
		deviceRoute("/features/widgets", "Home-screen widget", "Publish a public summary, then add this app's widget from your home screen. Publishing does not pin it or confirm OS display.", []nativeui.Action{
			{ID: "publish", Label: "Publish summary", Primary: true}, {ID: "status", Label: "Check snapshot"}, {ID: "clear", Label: "Clear summary"},
		}, map[string]nativeui.Handle{
			"publish": service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Publish, Snapshot: &widgets.Snapshot{Title: "GoFastr device features", Text: "Published from shared Go code", Route: "/features/widgets", Items: []widgets.Item{{ID: "shared", Title: "Android and iOS", Subtitle: "One Go snapshot"}}}}}),
			"status":  service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Status}}),
			"clear":   service(nativeui.ServiceRequest{Widget: &widgets.Request{Operation: widgets.Clear}}),
		}),
		deviceRoute("/features/push", "Push notifications", "Register to get this device's push token for your server. On Android received messages wait here until you acknowledge them; iOS opens a tapped notification's route directly and has no inbox.", []nativeui.Action{
			{ID: "register", Label: "Register for push", Primary: true}, {ID: "status", Label: "Check registration"}, {ID: "next", Label: "Read next message"}, {ID: "acknowledge", Label: "Acknowledge message"}, {ID: "unregister", Label: "Unregister"},
		}, map[string]nativeui.Handle{
			"register":   service(nativeui.ServiceRequest{Push: &push.Request{Operation: push.Register}}),
			"status":     service(nativeui.ServiceRequest{Push: &push.Request{Operation: push.Status}}),
			"next":       service(nativeui.ServiceRequest{Push: &push.Request{Operation: push.Next}}),
			"unregister": service(nativeui.ServiceRequest{Push: &push.Request{Operation: push.Unregister}}),
			"acknowledge": func(context.Context, nativeui.Request) (nativeui.Result, error) {
				inboxMu.Lock()
				defer inboxMu.Unlock()
				if pushed == nil {
					return nativeui.Result{Announcement: "Read a push message before acknowledging it."}, nil
				}
				return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "device", Push: &push.Request{Operation: push.Acknowledge, Identifier: pushed.Identifier}}}, nil
			},
		}),
		nativeui.Route{Path: "/features/refresh", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			return nativeui.Screen{Title: "Background refresh", LargeTitle: true, Text: "Schedule hourly background work, then leave the app. The OS decides when it runs, and may skip it. Last background refresh: " + lastRefresh() + ".", Actions: []nativeui.Action{
				{ID: "schedule", Label: "Schedule", Primary: true}, {ID: "status", Label: "Check schedule"}, {ID: "cancel", Label: "Cancel refresh"},
			}}, nil
		}, Actions: map[string]nativeui.Handle{
			"schedule": service(nativeui.ServiceRequest{Refresh: &refresh.Request{Operation: refresh.Schedule, IntervalMinutes: 60}}),
			"status":   service(nativeui.ServiceRequest{Refresh: &refresh.Request{Operation: refresh.Status}}),
			"cancel":   service(nativeui.ServiceRequest{Refresh: &refresh.Request{Operation: refresh.Cancel}}),
		}, Services: map[string]nativeui.ServiceRespond{"device": respond}},
		nativeui.Route{
			Path: "/features/links",
			Screen: func(_ context.Context, request nativeui.Request) (nativeui.Screen, error) {
				return nativeui.Screen{
					Title:    "Web links",
					Text:     "Open a web URL with Android. Link source: " + preview(request.Values["source"]),
					Actions:  []nativeui.Action{{ID: "open-web", Label: "Open Android App Links guide", Primary: true}},
					Requires: []nativeui.Requirement{{Capability: nativeui.CapabilityOpenURL}},
				}, nil
			},
			Actions: map[string]nativeui.Handle{
				"open-web": service(nativeui.ServiceRequest{OpenURL: &links.Open{URL: "https://developer.android.com/training/app-links"}}),
			},
			Services: map[string]nativeui.ServiceRespond{"device": func(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
				return nativeui.Result{Announcement: result.Message}, nil
			}},
		},
	}
	return nativeui.NewWithExtensions(Root(), []extension.Definition{barcodeext.Definition()}, append(routes, newLocationRoutes()...)...)
}

func demoToken() string {
	var input [32]byte
	rand.Read(input[:])
	return base64.RawURLEncoding.EncodeToString(input[:])
}

func preview(value string) string {
	runes := []rune(value)
	if len(runes) > 160 {
		return string(runes[:160]) + "..."
	}
	return value
}

func snapshotText(result deviceinfo.Result) string {
	if info := result.Info; info != nil {
		version := info.AppVersion
		if version == "" {
			version = "unknown"
		}
		return fmt.Sprintf("Device: %s %s; %s %s (API %d); locale %s; app %s.", info.Manufacturer, info.Model, info.Platform, info.OSVersion, info.APILevel, info.Locale, version)
	}
	if battery := result.Battery; battery != nil {
		level := "unknown"
		if battery.LevelKnown {
			level = fmt.Sprintf("%d%%", battery.Level)
		}
		save := "off"
		if battery.PowerSave {
			save = "on"
		}
		return fmt.Sprintf("Battery: %s %s (%s), power save %s.", level, battery.State, battery.PowerSource, save)
	}
	return "Device snapshot unavailable."
}

// calendarEditorRespond reports the Android editor outcome. The editor does
// not say whether the person saved the event.
func calendarEditorRespond(_ context.Context, _ nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
	if result.Status == nativeui.EffectCompleted {
		return nativeui.Result{Announcement: "Calendar editor closed. Saving is not confirmed."}, nil
	}
	return nativeui.Result{Announcement: "Calendar editor: " + string(result.Status)}, nil
}

func calendarMessage(result calendar.Result) string {
	if result.Authorization != "" {
		return "Calendar authorization: " + result.Authorization
	}
	if result.Identifier != "" {
		return "Sample event added."
	}
	if len(result.Events) == 0 {
		return "No calendar events in the next 7 days."
	}
	lines := make([]string, 0, len(result.Events))
	for _, event := range result.Events {
		lines = append(lines, event.Title+" — "+event.Start.Format(time.RFC3339))
	}
	return strings.Join(lines, "\n")
}
