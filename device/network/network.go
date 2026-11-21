// Package network defines one-shot observations of the active network.
package network

import "errors"

const Status = "status"

type Request struct {
	Operation string `json:"operation"`
}

type Result struct {
	Connected bool   `json:"connected"`
	Transport string `json:"transport"`
	Metered   bool   `json:"metered"`
	Validated bool   `json:"validated"`
}

func (request Request) Validate() error {
	if request.Operation != Status {
		return errors.New("network: only status is supported")
	}
	return nil
}

func (request Request) Accept(result Result) error {
	if err := request.Validate(); err != nil {
		return err
	}
	switch result.Transport {
	case "wifi", "cellular", "ethernet", "vpn", "other", "none":
	default:
		return errors.New("network: unknown transport")
	}
	if result.Connected == (result.Transport == "none") || !result.Connected && (result.Metered || result.Validated) {
		return errors.New("network: inconsistent connection status")
	}
	return nil
}
