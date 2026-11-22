package desktop

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/hajimehoshi/ebiten/v2"
)

func TestMain(m *testing.M) {
	// RunGame must run on the main thread. Keep the graphics check opt-in
	// so the model tests can also run without an interactive desktop.
	if os.Getenv("GOFASTR_DESKTOP_GPU_TEST") == "1" {
		if err := testDirectXWorkspace(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("DirectX workspace rendered and preserved the paused viewport during replay")
	}
	if os.Getenv("GOFASTR_DESKTOP_IME_TEST") == "1" {
		if err := testIMEPipeline(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("IME message pipeline passed: commits, caret moves, cancel, and astral input through the real window")
	}
	os.Exit(m.Run())
}

type gpuTestGame struct {
	*gpuWindow
	beforeDraw func()
	afterDraw  func(*ebiten.Image)
}

func TestFractionalDPIPaintsAndHitsSamePixels(t *testing.T) {
	for _, scale := range []dpiScale{1, 1.25, 1.5, 2} {
		for x := 0; x < 10; x++ {
			box := image.Rect(x, 1, x+1, 2)
			pixels := scale.rect(box)
			for px := pixels.Min.X; px < pixels.Max.X; px++ {
				if got := scale.logicalPoint(float64(px), float64(pixels.Min.Y)); got != box.Min {
					t.Fatalf("scale %v: physical column %d maps to %v, want %v", scale, px, got, box.Min)
				}
			}
		}
	}
}

func (g *gpuTestGame) Draw(screen *ebiten.Image) {
	if g.beforeDraw != nil {
		g.beforeDraw()
	}
	g.gpuWindow.Draw(screen)
	g.afterDraw(screen)
}

func TestGPUCompositionRoutesInputByNode(t *testing.T) {
	first, second, presses := 0, 0, 0
	enabled := false
	view := func() View {
		return View{Title: "Route input", Content: VStack(8,
			HStack(8,
				SelectBox("first", Choice{
					Options: []string{"All", "Error"}, Selected: first,
					Change: func(value int) { first = value },
				}),
				SelectBox("second", Choice{
					Options: []string{"Both", "Active"}, Selected: second,
					Change: func(value int) { second = value },
				}),
			).Height(34),
			PushButton("save", Button{Label: "Save", Press: func() { presses++ }}).Width(100),
			Switch("setting", Toggle{Label: "Enable", Checked: enabled, Change: func(value bool) { enabled = value }}),
		).Padding(10)}
	}
	window, err := newGPUWindow(t.Context(), view)
	if err != nil {
		t.Fatal(err)
	}
	window.scene.layout(image.Pt(720, 400))
	clickCenter := func(box image.Rectangle) { window.click(box.Min.Add(box.Size().Div(2))) }
	choice, _ := window.scene.find("second")
	clickCenter(choice.controlBox())
	if window.openChoice != "second" || window.scene.focus != "second" {
		t.Fatal("the second select box did not receive focus")
	}
	clickCenter(choice.optionBox(1))
	if first != 0 || second != 1 || window.openChoice != "" || presses != 0 {
		t.Fatal("the choice was routed to the wrong control or leaked to the button")
	}
	button, _ := window.scene.find("save")
	clickCenter(button.controlBox())
	if presses != 1 || window.scene.focus != "save" {
		t.Fatal("the button did not receive the click")
	}
	toggle, _ := window.scene.find("setting")
	clickCenter(toggle.controlBox())
	if !enabled || window.scene.focus != "setting" {
		t.Fatal("the switch did not receive the click")
	}
}

func testDirectXWorkspace() error {
	const initial = 1_000_000
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	count, selected, reads := initial, -1, 0
	updates := make(chan struct{}, 1)
	marker := color.RGBA{221, 123, 78, 255}
	window, err := newGPUWindow(ctx, func() View {
		table := Table{
			Key: "logs", Count: count, Follow: selected < 0, Selected: selected,
			Columns: []Column{{Title: "Message", Width: 270}},
			Cell: func(row, _ int) string {
				reads++
				return fmt.Sprintf("Entry %d", row+1)
			},
			Select: func(row int) { selected = row },
		}
		left, right := VirtualTable("logs", table), VStack(4,
			Paint(func(c Canvas) { c.Fill(c.Bounds().Inset(-4), marker) }).Height(12),
			Details("details", Detail{Title: "Entry details", Text: "Paused on the selected log entry."}),
		)
		content := Split("workspace", 68, left, right)
		if count >= initial+25_000 {
			content = Split("workspace", 68, right, left)
		}
		return View{Title: "Log Explorer graphics test", Content: content}
	})
	if err != nil {
		return err
	}
	var failure error
	var checkedDPI bool
	var anchor, drawn int
	game := &gpuTestGame{gpuWindow: window, beforeDraw: func() { reads = 0 }, afterDraw: func(screen *ebiten.Image) {
		item, _ := window.scene.find("logs")
		v := item.state.viewport
		rows, _ := item.tableBounds()
		table := item.node.value.(Table)
		var info ebiten.DebugInfo
		ebiten.ReadDebugInfo(&info)
		if info.GraphicsLibrary != ebiten.GraphicsLibraryDirectX {
			failure = fmt.Errorf("expected DirectX, got %v", info.GraphicsLibrary)
			cancel()
			return
		}
		if reads < 1 || reads > v.page {
			failure = fmt.Errorf("draw requested %d cells for %d visible rows", reads, v.page)
			cancel()
			return
		}
		reads = 0
		if !checkedDPI {
			var client rect
			var dpi uintptr
			ebiten.RunOnMainThread(func() {
				if hwnd := accFindAppWindow(); hwnd != 0 {
					getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&client)))
					dpi, _, _ = getDPI.Call(hwnd)
				}
			})
			if dpi != 0 {
				checkedDPI = true
				if want := image.Pt(int(client.Right-client.Left), int(client.Bottom-client.Top)); screen.Bounds().Size() != want {
					failure = fmt.Errorf("GPU buffer %v does not match the Windows client area %v", screen.Bounds().Size(), want)
					cancel()
					return
				}
				if want := window.theme.body.Size * float64(dpi) / 96; window.paintTheme.body.Size != want {
					failure = fmt.Errorf("body font size = %g physical pixels, want %g at %d DPI", window.paintTheme.body.Size, want, dpi)
					cancel()
					return
				}
			}
		}
		if drawn == 0 {
			anchor = v.top
			window.click(rows.Min.Add(rows.Size().Div(4)))
		} else if v.top != anchor || table.Selected < 0 || window.scene.focus != "logs" {
			failure = errors.New("replay moved the paused viewport or lost selection")
			cancel()
			return
		}
		drawn++
		if table.Count == initial+50_000 {
			y := rows.Min.Y + (selected-anchor)*rowHeight
			// Inspect the selected-row indicator in the rendered back buffer.
			point := window.deviceScale.pixel(float64(rows.Min.X+1), float64(y+1))
			if got := color.RGBAModel.Convert(screen.At(point.X, point.Y)); got != accent {
				failure = fmt.Errorf("selected row indicator = %v, want %v", got, accent)
			}
			point = window.deviceScale.pixel(2, 2)
			if got := color.RGBAModel.Convert(screen.At(point.X, point.Y)); got != marker {
				failure = fmt.Errorf("custom canvas pixel = %v, want %v", got, marker)
			}
			point = window.deviceScale.pixel(2, 13)
			if got := color.RGBAModel.Convert(screen.At(point.X, point.Y)); got == marker {
				failure = errors.New("custom canvas drew outside its bounds")
			}
			cancel()
			return
		}
		if count == table.Count {
			count += 5_000
			updates <- struct{}{}
		}
	}}
	if err := runGPUWindow(window, updates, game); err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if !checkedDPI {
		return errors.New("could not read DPI for the DirectX test window")
	}
	if item, found := window.scene.find("logs"); !found || item.node.value.(Table).Count != initial+50_000 || drawn < 11 {
		return fmt.Errorf("GPU run ended at %d entries after %d draws", count, drawn)
	}
	return nil
}
