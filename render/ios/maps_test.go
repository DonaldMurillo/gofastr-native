package ios

import (
	"os"
	"path/filepath"
	"testing"
)

func mapsSource(t *testing.T, directory string) string {
	t.Helper()
	data, err := os.ReadFile("../../extension/maps/GofastrMaps.swift")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "GofastrMaps.swift")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMapsUIKitOptimizedCompiles(t *testing.T) {
	directory := t.TempDir()
	args := uikitSimulatorArguments(t, directory, "-O", "-parse-as-library", "-emit-library", "-module-name", "GofastrMapsChecks")
	args = append(args, swiftDirectorySources(t, directory, Sources)...)
	catalog := filepath.Join(directory, "Catalog.swift")
	if err := os.WriteFile(catalog, []byte(`import UIKit
func installMapsCatalog() {
 precondition(GofastrMapComponent.name == "gofastr.maps/v1")
 MobileComponentExtensions.install([GofastrMapComponent.self])
}
`), 0600); err != nil {
		t.Fatal(err)
	}
	args = append(args, mapsSource(t, directory), catalog, "-o", filepath.Join(directory, "libMaps.dylib"))
	requireXcrun(t, "maps-enabled UIKit library must compile and link", args...)
}

func TestMapsFoundationDecode(t *testing.T) {
	requireSwift(t)
	directory := t.TempDir()
	args := append([]string{"-O"}, swiftSources(t, directory, Sources, rendererSourceFiles...)...)
	args = append(args, mapsSource(t, directory))
	compileRunChecks(t, directory, "maps-checks", "import Foundation\n"+draftDTOs(t)+`
@main struct MapsChecks {
 static func main() throws {
 let value = try GofastrMapsPayload.decode(#"{"pins":[{"id":"a","title":"Place","subtitle":"Plain","latitude":90,"longitude":180}],"showsUserLocation":true}"#)
 precondition(value.pins?.first?.title == "Place" && value.showsUserLocation)
 for payload in ["", "{}", #"{"pins":[],"showsUserLocation":false}"#, #"{"region":{"latitude":91,"longitude":0,"latitudeDelta":1,"longitudeDelta":1},"showsUserLocation":false}"#] {
 do { _ = try GofastrMapsPayload.decode(payload); preconditionFailure("invalid payload accepted") } catch {}
 }
 }
}
`, "maps wire executable must compile", "maps payload regression", args, nil)
}
