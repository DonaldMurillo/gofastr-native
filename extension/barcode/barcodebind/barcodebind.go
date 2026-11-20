// Package barcodebind is the gomobile boundary for the Android scanner.
package barcodebind

import (
	"encoding/json"

	"github.com/DonaldMurillo/gofastr-native/device/barcode"
)

func MaxDimension() int { return barcode.MaxDimension }

func Decode(pixels []byte, width, height int) (string, error) {
	result, err := barcode.DecodeLuminance(pixels, width, height)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(result)
	return string(data), err
}
