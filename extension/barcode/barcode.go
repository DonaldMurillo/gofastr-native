// Package barcode is the compiled Android adapter for device/scanner.
//
// Register Definition with nativeui.NewWithExtensions and request scans with
// nativeui.ServiceRequest.Scan, the same scanner/v1 service the native iOS
// renderer implements. The adapter is opt-in so apps that never scan do not
// link the decoder.
package barcode

import (
	"embed"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

//go:embed gofastr-native.extension.json GofastrBarcode.java
var assets embed.FS

var errService = errors.New("barcode: request scans with nativeui.ServiceRequest.Scan")

// Definition registers the Android scanner/v1 adapter. It serves service
// requests only, so it rejects every file operation.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{
		Manifest: manifest, Assets: assets,
		Validate: func(extension.FileOperation) (string, error) { return "", errService },
		Accept:   func(extension.FileOperation, files.Document) error { return errService },
	}
}
