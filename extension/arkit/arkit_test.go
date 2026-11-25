package arkit_test

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/extension"
	"github.com/DonaldMurillo/gofastr-native/extension/arkit"
	"github.com/DonaldMurillo/gofastr-native/nativeui"
)

func valid() arkit.Content {
	return arkit.Content{
		Planes:      []arkit.Plane{arkit.Horizontal},
		ShowsPlanes: true,
		Objects: []arkit.Object{
			{ID: "cube", Title: "<Cube>", Shape: arkit.Box, Size: 0.2, Color: "#1a2B3c", Position: &arkit.Vector{Z: -1}, Destination: "/detail"},
			{ID: "ball", Title: "Ball", Shape: arkit.Sphere, Size: 0.1, Color: "#ffffff"},
		},
	}
}

func TestValidation(t *testing.T) {
	registry, err := extension.New(arkit.Definition())
	if err != nil {
		t.Fatal(err)
	}
	block, err := arkit.Block("scene", valid())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateComponent(block); err != nil {
		t.Fatal(err)
	}
	if len(block.Destinations) != 1 || block.Destinations["object.cube"] != "/detail" {
		t.Fatalf("destinations = %v", block.Destinations)
	}
	for name, change := range map[string]func(*arkit.Content){
		"duplicate-plane":    func(c *arkit.Content) { c.Planes = append(c.Planes, arkit.Horizontal) },
		"unknown-plane":      func(c *arkit.Content) { c.Planes = []arkit.Plane{"diagonal"} },
		"planes-hidden-none": func(c *arkit.Content) { c.Planes = nil; c.Objects = c.Objects[:1] },
		"unplaced-no-planes": func(c *arkit.Content) { c.Planes = nil; c.ShowsPlanes = false },
		"id":                 func(c *arkit.Content) { c.Objects[0].ID = "../bad" },
		"long-id":            func(c *arkit.Content) { c.Objects[0].ID = strings.Repeat("a", 65) },
		"duplicate-id":       func(c *arkit.Content) { c.Objects[1].ID = "cube" },
		"empty-title":        func(c *arkit.Content) { c.Objects[0].Title = "" },
		"control-title":      func(c *arkit.Content) { c.Objects[0].Title = "a\nb" },
		"long-title":         func(c *arkit.Content) { c.Objects[0].Title = strings.Repeat("x", 257) },
		"shape":              func(c *arkit.Content) { c.Objects[0].Shape = "torus" },
		"color":              func(c *arkit.Content) { c.Objects[0].Color = "#12345g" },
		"short-color":        func(c *arkit.Content) { c.Objects[0].Color = "#fff" },
		"small":              func(c *arkit.Content) { c.Objects[0].Size = 0.001 },
		"large":              func(c *arkit.Content) { c.Objects[0].Size = 2.5 },
		"nan-size":           func(c *arkit.Content) { c.Objects[0].Size = math.NaN() },
		"far":                func(c *arkit.Content) { c.Objects[0].Position = &arkit.Vector{X: 51} },
		"infinite":           func(c *arkit.Content) { c.Objects[0].Position = &arkit.Vector{Y: math.Inf(-1)} },
		"destination":        func(c *arkit.Content) { c.Objects[0].Destination = strings.Repeat("/", 4097) },
		"too-many": func(c *arkit.Content) {
			c.Objects = nil
			for i := 0; i <= arkit.MaxObjects; i++ {
				c.Objects = append(c.Objects, arkit.Object{ID: fmt.Sprintf("o%d", i), Title: "O", Shape: arkit.Cone, Size: 0.1, Color: "#000000"})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := valid()
			change(&c)
			if _, err := arkit.Block("scene", c); err == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
	c := valid()
	c.Objects = nil
	for i := 0; i < arkit.MaxObjects; i++ {
		c.Objects = append(c.Objects, arkit.Object{ID: fmt.Sprintf("o%d", i), Title: "O", Shape: arkit.Pyramid, Size: 2, Color: "#000000", Position: &arkit.Vector{X: -50, Y: 50}, Destination: "/detail"})
	}
	block, err = arkit.Block("scene", c)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.ValidateComponent(block); err != nil {
		t.Fatal(err)
	}
	block.Destinations["object.o0"] = "/forged"
	if err := registry.ValidateComponent(block); err == nil {
		t.Fatal("forged destination accepted")
	}
	delete(block.Destinations, "object.o0")
	if err := registry.ValidateComponent(block); err == nil {
		t.Fatal("missing destination accepted")
	}
	if _, err := arkit.Block("scene", arkit.Content{LightEstimation: true}); err != nil {
		t.Fatal("empty tracking scene rejected:", err)
	}
	if _, err := arkit.Block("bad/id", valid()); err == nil {
		t.Fatal("invalid component ID accepted")
	}
}

func TestIOSOnlyNegotiation(t *testing.T) {
	definition := arkit.Definition()
	if err := definition.Manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if definition.Manifest.Name != arkit.Name || !definition.Manifest.HasIOS() || definition.Manifest.HasWebView() || definition.Manifest.Android != nil {
		t.Fatal("unexpected adapter")
	}
	block, _ := arkit.Block("scene", valid())
	app, err := nativeui.NewWithExtensions("/", []extension.Definition{definition}, nativeui.Route{Path: "/", Screen: func(context.Context, nativeui.Request) (nativeui.Screen, error) {
		return nativeui.Screen{Title: "AR", Components: []extension.Component{block}}, nil
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
	base := map[nativeui.Capability]nativeui.Support{nativeui.CapabilityNavigation: nativeui.SupportAvailable, nativeui.CapabilityEditor: nativeui.SupportAvailable}
	for _, renderer := range []string{"android", "webview"} {
		hello := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: renderer, Capabilities: base}})
		if hello.Error != nil {
			t.Fatal(hello.Error)
		}
		if opened := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/"}); opened.Error == nil {
			t.Fatal("unsupported AR scene opened on", renderer)
		}
	}
	base[arkit.Name] = nativeui.SupportAvailable
	hello := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "hello", Hello: &nativeui.Hello{Minimum: 1, Maximum: 1, Renderer: "uikit", Capabilities: base}})
	if hello.Error != nil {
		t.Fatal(hello.Error)
	}
	if opened := runtime.Handle(context.Background(), nativeui.Command{Version: 1, Kind: "open", Connection: hello.Connection, Route: "/"}); opened.Error != nil {
		t.Fatal(opened.Error)
	}
}
