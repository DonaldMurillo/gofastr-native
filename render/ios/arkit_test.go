package ios

import (
	"os"
	"path/filepath"
	"testing"
)

func arkitSource(t *testing.T, directory string) string {
	t.Helper()
	data, err := os.ReadFile("../../extension/arkit/GofastrAR.swift")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "GofastrAR.swift")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestARKitUIKitOptimizedCompiles(t *testing.T) {
	directory := t.TempDir()
	args := uikitSimulatorArguments(t, directory, "-O", "-parse-as-library", "-emit-library", "-module-name", "GofastrARKitChecks")
	args = append(args, swiftDirectorySources(t, directory, Sources)...)
	catalog := filepath.Join(directory, "Catalog.swift")
	if err := os.WriteFile(catalog, []byte(`import UIKit
func installARKitCatalog() {
 precondition(GofastrARComponent.name == "gofastr.arkit/v1")
 MobileComponentExtensions.install([GofastrARComponent.self])
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	args = append(args, arkitSource(t, directory), catalog, "-o", filepath.Join(directory, "libARKit.dylib"))
	requireXcrun(t, "ARKit-enabled UIKit library must compile and link", args...)
}

func TestARKitFoundationDecode(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	args := append([]string{"-O"}, swiftSources(t, directory, Sources, rendererSourceFiles...)...)
	args = append(args, arkitSource(t, directory))
	compileRunChecks(t, directory, "arkit-checks", "import Foundation\n"+draftDTOs(t)+`
@main struct ARKitChecks {
 static func main() throws {
 let value = try GofastrARPayload.decode(##"{"planes":["horizontal","vertical"],"showsPlanes":true,"coaching":true,"lightEstimation":false,"objects":[{"id":"cube","title":"Cube","shape":"box","size":0.2,"color":"#1A2b3C","position":{"x":0,"y":-0.5,"z":-50},"destination":"/x"},{"id":"ball","title":"Ball","shape":"sphere","size":2,"color":"#ffffff"}]}"##)
 precondition(value.objects?.count == 2 && value.objects?[0].navigable == true && value.objects?[1].navigable == false && value.objects?[1].position == nil)
 precondition(GofastrARPayload.rgb("#1A2b3C")! == (0x1a / 255.0, 0x2b / 255.0, 0x3c / 255.0))
 for color in ["#12345", "#+12345", "123456", "#12345g", "#１２３４５６"] { precondition(GofastrARPayload.rgb(color) == nil, color) }
 let none = try GofastrARPayload.decode(##"{"showsPlanes":false,"coaching":false,"lightEstimation":true}"##)
 precondition(none.objects == nil && none.planes == nil)
 for payload in ["", "{}",
  ##"{"planes":["horizontal","horizontal"],"showsPlanes":false,"coaching":false,"lightEstimation":false}"##,
  ##"{"planes":["diagonal"],"showsPlanes":false,"coaching":false,"lightEstimation":false}"##,
  ##"{"showsPlanes":true,"coaching":false,"lightEstimation":false}"##,
  ##"{"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"A","shape":"box","size":0.1,"color":"#000000"}]}"##,
  ##"{"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"A","shape":"torus","size":0.1,"color":"#000000","position":{"x":0,"y":0,"z":0}}]}"##,
  ##"{"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"A","shape":"box","size":3,"color":"#000000","position":{"x":0,"y":0,"z":0}}]}"##,
  ##"{"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"A","shape":"box","size":0.1,"color":"#000000","position":{"x":51,"y":0,"z":0}}]}"##,
  ##"{"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"","shape":"box","size":0.1,"color":"#000000","position":{"x":0,"y":0,"z":0}}]}"##,
  ##"{"planes":["horizontal"],"showsPlanes":false,"coaching":false,"lightEstimation":false,"objects":[{"id":"a","title":"A","shape":"box","size":0.1,"color":"#000000"},{"id":"a","title":"B","shape":"box","size":0.1,"color":"#000000"}]}"##] {
 do { _ = try GofastrARPayload.decode(payload); preconditionFailure("invalid payload accepted: \(payload)") } catch {}
 }
 precondition(GofastrARStatus.message(tracking: "Limited", surfaces: 3, detectsPlanes: true, unplaced: "Ball", navigable: true) == "Limited")
 precondition(GofastrARStatus.message(tracking: nil, surfaces: 0, detectsPlanes: true, unplaced: "Ball", navigable: true) == "Move the device slowly to find a surface.")
 precondition(GofastrARStatus.message(tracking: nil, surfaces: 1, detectsPlanes: true, unplaced: "Ball", navigable: false) == "Tap a surface to place Ball.")
 precondition(GofastrARStatus.message(tracking: nil, surfaces: 1, detectsPlanes: true, unplaced: nil, navigable: true) == "Tap an object to open it.")
 precondition(GofastrARStatus.message(tracking: nil, surfaces: 2, detectsPlanes: true, unplaced: nil, navigable: false) == "Found 2 surfaces.")
 precondition(GofastrARStatus.message(tracking: nil, surfaces: 0, detectsPlanes: false, unplaced: nil, navigable: false) == "Tracking.")
 }
}
`, "ARKit wire executable must compile", "ARKit payload regression", args, nil)
}
