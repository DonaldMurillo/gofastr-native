// Package scanner defines bounded, explicitly initiated barcode scanning.
package scanner

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

type Symbology string

const (
	QR         Symbology = "qr"
	EAN13      Symbology = "ean13"
	EAN8       Symbology = "ean8"
	UPCE       Symbology = "upce"
	Code39     Symbology = "code39"
	Code93     Symbology = "code93"
	Code128    Symbology = "code128"
	ITF14      Symbology = "itf14"
	PDF417     Symbology = "pdf417"
	Aztec      Symbology = "aztec"
	DataMatrix Symbology = "datamatrix"
)

type Request struct {
	Title       string      `json:"title"`
	Symbologies []Symbology `json:"symbologies,omitempty"`
}

type Result struct {
	Value     string    `json:"value"`
	Symbology Symbology `json:"symbology"`
}

func known(value Symbology) bool {
	switch value {
	case QR, EAN13, EAN8, UPCE, Code39, Code93, Code128, ITF14, PDF417, Aztec, DataMatrix:
		return true
	}
	return false
}

func (request Request) Validate() error {
	if strings.TrimSpace(request.Title) == "" || !uitext.Plain(request.Title, 200) {
		return errors.New("scanner: a plain title of at most 200 bytes is required")
	}
	seen := make(map[Symbology]bool)
	for _, value := range request.Symbologies {
		if !known(value) || seen[value] {
			return errors.New("scanner: unknown or duplicate symbology")
		}
		seen[value] = true
	}
	return nil
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if result.Value == "" || !utf8.ValidString(result.Value) || len(result.Value) > 4096 || !known(result.Symbology) {
		return errors.New("scanner: invalid barcode result")
	}
	if len(request.Symbologies) == 0 {
		return nil
	}
	for _, value := range request.Symbologies {
		if value == result.Symbology {
			return nil
		}
	}
	return errors.New("scanner: unrequested symbology")
}
