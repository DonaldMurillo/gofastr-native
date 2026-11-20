package maps_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/maps"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func valid() maps.Content {
	return maps.Content{Pins: []maps.Pin{{ID: "one", Title: "<Plain text>", Latitude: 90, Longitude: -180, Destination: "/detail"}}}
}

func TestValidation(t *testing.T) {
	for _, length := range []int{64, 65} {
		content := valid()
		content.Pins[0].ID = strings.Repeat("p", length)
		block, err := maps.Block("map", content)
		if length == 65 {
			if err == nil {
				t.Fatal("overlong pin ID accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		registry, err := extension.New(maps.Definition())
		if err != nil {
			t.Fatal(err)
		}
		if err := registry.ValidateComponent(block); err != nil {
			t.Fatal(err)
		}
	}
	for name, change := range map[string]func(*maps.Content){
		"empty":             func(c *maps.Content) { c.Pins = nil },
		"latitude":          func(c *maps.Content) { c.Pins[0].Latitude = 91 },
		"longitude":         func(c *maps.Content) { c.Pins[0].Longitude = -181 },
		"nan":               func(c *maps.Content) { c.Pins[0].Latitude = math.NaN() },
		"infinity":          func(c *maps.Content) { c.Pins[0].Longitude = math.Inf(1) },
		"id":                func(c *maps.Content) { c.Pins[0].ID = "../bad" },
		"duplicate":         func(c *maps.Content) { c.Pins = append(c.Pins, c.Pins[0]) },
		"title":             func(c *maps.Content) { c.Pins[0].Title = strings.Repeat("x", 257) },
		"subtitle":          func(c *maps.Content) { c.Pins[0].Subtitle = "bad\x00text" },
		"region":            func(c *maps.Content) { c.Region = &maps.Region{LatitudeDelta: 0, LongitudeDelta: 1} },
		"delta":             func(c *maps.Content) { c.Region = &maps.Region{LatitudeDelta: 181, LongitudeDelta: 361} },
		"region-coordinate": func(c *maps.Content) { c.Region = &maps.Region{Latitude: 91, LatitudeDelta: 1, LongitudeDelta: 1} },
		"too-many":          func(c *maps.Content) { c.Pins = make([]maps.Pin, 201) },
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			change(&c)
			if _, err := maps.Block("map", c); err == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
	if _, err := maps.Block("map", maps.Content{Region: &maps.Region{Latitude: -90, Longitude: 180, LatitudeDelta: 180, LongitudeDelta: 360}}); err != nil {
		t.Fatal(err)
	}
	registry, err := extension.New(maps.Definition())
	if err != nil {
		t.Fatal(err)
	}
	c := valid()
	c.Pins = nil
	for i := 0; i < 200; i++ {
		c.Pins = append(c.Pins, maps.Pin{ID: fmt.Sprintf("p%d", i), Destination: "/detail"})
	}
	block, err := maps.Block("map", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateComponent(block); err != nil {
		t.Fatal(err)
	}
	block.Destinations["pin.p0"] = "/forged"
	if err := registry.ValidateComponent(block); err == nil {
		t.Fatal("forged destination accepted")
	}
}

func TestIOSOnlyNegotiation(t *testing.T) {
	definition := maps.Definition()
	if err := definition.Manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if !definition.Manifest.HasIOS() || definition.Manifest.HasWebView() || definition.Manifest.Android != nil {
		t.Fatal("unexpected adapter")
	}
	block, _ := maps.Block("map", valid())
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{definition}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Map", Components: []extension.Component{block}}, nil
	}}, nativeui.Route{Path: "/detail", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "Detail"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := nativeui.NewRuntime(app, nativeui.RuntimeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, renderer := range []string{"android", "webview"} {
		hello := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: renderer, Capabilities: map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable}}})
		if hello.Error != nil {
			t.Fatal(hello.Error)
		}
		opened := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/"})
		if opened.Error == nil {
			t.Fatal("unsupported map opened")
		}
	}
}
