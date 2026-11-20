// Package contacts registers the compiled Android contact picker.
package contacts

import (
	"embed"
	"encoding/json"

	policy "github.com/DonaldMurillo/gofastr-native/device/contacts"
	"github.com/DonaldMurillo/gofastr-native/device/files"
	"github.com/DonaldMurillo/gofastr-native/extension"
)

//go:embed gofastr-native.extension.json *.java
var assets embed.FS

type Request = policy.Request
type Kind = policy.Kind
type Contact = policy.Contact

const (
	Phone = policy.Phone
	Email = policy.Email
)

func Pick(request Request) (*extension.FileOperation, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	return &extension.FileOperation{Name: "gofastr.contacts/v1", Payload: string(payload)}, nil
}

func request(operation extension.FileOperation) (Request, error) {
	var request Request
	if err := extension.Decode(operation.Payload, &request); err != nil {
		return request, err
	}
	return request, request.Validate()
}

func Definition() extension.Definition {
	content, _ := assets.ReadFile(extension.ManifestName)
	manifest, _ := extension.ParseManifest(content)
	return extension.Definition{Manifest: manifest, Assets: assets,
		Validate: func(operation extension.FileOperation) (string, error) {
			_, err := request(operation)
			return "contacts/v1", err
		},
		Accept: func(operation extension.FileOperation, document files.Document) error {
			request, err := request(operation)
			if err != nil {
				return err
			}
			return request.Accept(document)
		},
	}
}
