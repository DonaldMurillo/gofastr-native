// Package links describes user-initiated requests to open a web URL.
package links

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/device/browser"
)

// Open requests a handoff to the operating system's HTTP(S) handler.
type Open struct {
	URL string `json:"url"`
}

var hostLabel = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

// Validate checks the URL before it can be admitted as a device service.
// It accepts only URLs that browser.Request also accepts, then also checks
// the host and port, because the URL leaves the app.
func (request Open) Validate() error {
	invalid := errors.New("links: a valid HTTP or HTTPS URL without credentials is required")
	value := request.URL
	if (browser.Request{URL: value}).Validate() != nil || strings.Contains(value, "\\") {
		return invalid
	}
	if _, err := url.PathUnescape(value); err != nil {
		return invalid
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || strings.HasSuffix(parsed.Host, ":") || !validHost(parsed.Hostname()) {
		return invalid
	}
	if port := parsed.Port(); port != "" {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return invalid
		}
	}
	return nil
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	host = strings.TrimSuffix(host, ".")
	labels := strings.Split(host, ".")
	last := labels[len(labels)-1]
	if len(host) > 253 || len(labels) > 1 && (last == "" || last[0] >= '0' && last[0] <= '9') {
		return false
	}
	for _, label := range labels {
		if !hostLabel.MatchString(label) {
			return false
		}
	}
	return true
}
