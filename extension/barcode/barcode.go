// Package barcode registers the compiled Android barcode scanner.
package barcode

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"

	"github.com/DonaldMurillo/gofastr-native/device/barcode"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

//go:embed gofastr-native.extension.json GofastrBarcode.java
var assets embed.FS

const (
	name     = "gofastr.barcode/v1"
	filename = "Barcode.json"
)

// Scan builds an Android-only operation for use with nativeui.FileRequest.
func Scan(request barcode.Request) (*extension.FileOperation, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return &extension.FileOperation{Name: name, Payload: string(payload)}, nil
}

// ReadResult extracts the bounded code from a completed scan document.
func ReadResult(document files.Document) (barcode.Result, error) {
	var result barcode.Result
	if document.Name != filename || document.MediaType != "application/json" {
		return result, errors.New("barcode: expected a scan result document")
	}
	if err := document.Validate(); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(document.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return result, errors.New("barcode: trailing scan result content")
	}
	return result, result.Validate()
}

// Definition registers the scanner and its authoritative Go validators.
func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{
		Manifest: manifest, Assets: assets,
		Validate: func(operation extension.FileOperation) (string, error) {
			var request barcode.Request
			if err := extension.Decode(operation.Payload, &request); err != nil {
				return "", err
			}
			if err := request.Validate(); err != nil {
				return "", err
			}
			return "gofastr.barcode." + string(request.Source) + "/v1", nil
		},
		Accept: func(_ extension.FileOperation, document files.Document) error {
			_, err := ReadResult(document)
			return err
		},
	}
}
