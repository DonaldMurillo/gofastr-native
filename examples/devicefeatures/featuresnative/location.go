package featuresnative

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr-native/device/location"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func newLocationRoutes() []nativeui.Route {
	var mu sync.Mutex
	results := map[string]string{}
	labels := map[string]string{"status": "Status", "authorize": "Authorize", "current": "Current position", "track": "Track 5 samples"}
	handlers := map[string]nativeui.Handle{}
	for _, operation := range []location.Operation{location.Status, location.Authorize, location.Current, location.Track} {
		request := location.Request{Operation: operation}
		if operation == location.Track {
			request.MaxSamples, request.DurationMillis = 5, 5000
		}
		handlers[string(operation)] = func(context.Context, nativeui.Request) (nativeui.Result, error) {
			return nativeui.Result{Service: &nativeui.ServiceRequest{ID: "location", Location: &request}}, nil
		}
	}
	main := nativeui.Route{Path: "/features/location", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		mu.Lock()
		defer mu.Unlock()
		rows := []nativeui.Row{}
		for _, operation := range []string{"status", "authorize", "current", "track"} {
			text := results[operation]
			if text == "" {
				text = "Not requested"
			}
			rows = append(rows, nativeui.Row{ID: "location-" + operation, Title: labels[operation], Subtitle: text, Destination: "/features/location/" + operation})
		}
		return nativeui.Screen{Title: "Location", List: &nativeui.List{Header: "Foreground only. Tracking collects up to 5 samples over 5 seconds. Coordinates stay on this screen and are not saved.", Rows: rows}, Actions: []nativeui.Action{
			{ID: "current", Label: "Current position", Primary: true}, {ID: "status", Label: "Location status"}, {ID: "authorize", Label: "Authorize location"}, {ID: "track", Label: "Track 5 samples"},
		}}, nil
	}, Actions: handlers, Services: map[string]nativeui.ServiceRespond{"location": func(_ context.Context, request nativeui.Request, result nativeui.ServiceResult) (nativeui.Result, error) {
		text := string(result.Status)
		if result.Location != nil {
			observed := result.Location
			text += fmt.Sprintf("; permission: %s; services: %t; precise: %t; samples: %d", observed.Permission, observed.ServicesEnabled, observed.Precise, len(observed.Fixes))
			coordinates := []string{}
			for _, fix := range observed.Fixes {
				coordinates = append(coordinates, fmt.Sprintf("%.4f, %.4f (±%.0f m)", fix.Latitude, fix.Longitude, fix.HorizontalAccuracyMeters))
			}
			if len(coordinates) > 0 {
				text = strings.Join(coordinates, "\n") + "\n" + text
			}
		} else if result.Permission != "" {
			text += "; permission: " + result.Permission
		}
		mu.Lock()
		results[string(request.Service.Location.Operation)] = text
		mu.Unlock()
		return nativeui.Result{Announcement: strings.ReplaceAll(text, "\n", ". ")}, nil
	}}}
	routes := []nativeui.Route{main}
	for _, operation := range []string{"status", "authorize", "current", "track"} {
		routes = append(routes, nativeui.Route{Path: "/features/location/" + operation, Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
			mu.Lock()
			defer mu.Unlock()
			text := results[operation]
			if text == "" {
				text = "Not requested"
			}
			return nativeui.Screen{Title: labels[operation], Text: text}, nil
		}})
	}
	return routes
}
