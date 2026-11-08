package repocheck

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// AllowedLicenses are the dependency licenses compatible with shipping this
// MIT-licensed project without extra obligations beyond keeping notices.
var AllowedLicenses = []string{"MIT", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "ISC", "Unicode-3.0"}

var space = regexp.MustCompile(`\s+`)

// ClassifyLicense names the license in a license file's text, or returns
// "unknown". Copyleft licenses are checked first because some files carry
// several licenses.
func ClassifyLicense(text string) string {
	t := strings.ToLower(space.ReplaceAllString(text, " "))
	switch {
	case strings.Contains(t, "gnu affero general public license"):
		return "AGPL"
	case strings.Contains(t, "gnu lesser general public license"), strings.Contains(t, "gnu library general public license"):
		return "LGPL"
	case strings.Contains(t, "gnu general public license"):
		return "GPL"
	case strings.Contains(t, "mozilla public license"):
		return "MPL-2.0"
	case strings.Contains(t, "eclipse public license"):
		return "EPL"
	case strings.Contains(t, "server side public license"), strings.Contains(t, "business source license"):
		return "source-available"
	case strings.Contains(t, "apache license") && strings.Contains(t, "version 2.0"):
		return "Apache-2.0"
	case strings.Contains(t, "redistribution and use in source and binary forms"):
		if strings.Contains(t, "neither the name") || strings.Contains(t, "names of its contributors") {
			return "BSD-3-Clause"
		}
		return "BSD-2-Clause"
	case strings.Contains(t, "permission is hereby granted, free of charge"):
		return "MIT"
	case strings.Contains(t, "permission to use, copy, modify, and/or distribute this software for any purpose"),
		strings.Contains(t, "permission to use, copy, modify, and distribute this software for any purpose"):
		return "ISC"
	case strings.Contains(t, "unicode license v3"), strings.Contains(t, "unicode, inc. license agreement"):
		return "Unicode-3.0"
	}
	return "unknown"
}

// ModuleLicense finds and classifies the license file at the top of a
// module directory. It returns "" for the file name when there is none.
func ModuleLicense(dir string) (file, license string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "unknown"
	}
	var names []string
	for _, e := range entries {
		n := strings.ToLower(e.Name())
		if !e.IsDir() && (strings.HasPrefix(n, "license") || strings.HasPrefix(n, "licence") || strings.HasPrefix(n, "copying")) {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", "unknown"
	}
	slices.Sort(names)
	data, err := os.ReadFile(filepath.Join(dir, names[0]))
	if err != nil {
		return names[0], "unknown"
	}
	return names[0], ClassifyLicense(string(data))
}
