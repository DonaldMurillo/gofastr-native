package main

import (
	"fmt"
	"regexp"
	"strings"
)

var appLinkLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func parseAppLinkHosts(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var hosts []string
	seen := make(map[string]bool)
	for _, host := range strings.Split(value, ",") {
		host = strings.TrimSpace(host)
		labels := strings.Split(host, ".")
		last := labels[len(labels)-1]
		if len(host) > 253 || len(labels) < 2 || last == "" || last[0] < 'a' || last[0] > 'z' {
			return nil, fmt.Errorf("--app-link-hosts expects lowercase DNS hosts, got %q", host)
		}
		for _, label := range labels {
			if !appLinkLabel.MatchString(label) {
				return nil, fmt.Errorf("--app-link-hosts expects lowercase DNS hosts, got %q", host)
			}
		}
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
}
