// Package barcodebind is the gomobile boundary for the Android scanner.
package barcodebind

import (
	"encoding/json"
	"errors"

	"github.com/DonaldMurillo/gofastr-native/device/scanner"
	"github.com/DonaldMurillo/gofastr-native/extension/barcode/internal/decode"
)

// MaxDimension bounds each side of the luminance image passed to Decode.
func MaxDimension() int { return decode.MaxDimension }

// Supports reports whether a valid scanner/v1 request names at least one
// symbology this decoder reads.
func Supports(request string) bool {
	parsed, err := parse(request)
	return err == nil && len(decode.Supported(parsed)) > 0
}

// Decode returns the scanner/v1 result JSON for one code the request accepts,
// or an empty string when the image holds no such code.
func Decode(request string, pixels []byte, width, height int) (string, error) {
	parsed, err := parse(request)
	if err != nil {
		return "", err
	}
	result, err := decode.Luminance(parsed, pixels, width, height)
	if errors.Is(err, decode.ErrNotFound) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	return string(data), err
}

func parse(request string) (scanner.Request, error) {
	var parsed scanner.Request
	if err := json.Unmarshal([]byte(request), &parsed); err != nil {
		return parsed, err
	}
	return parsed, parsed.Validate()
}
