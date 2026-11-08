package main

import "github.com/DonaldMurillo/gofastr-native/tui/cmd/chordharp/harp"

// keyRows maps the keyboard to the grid: the top letter row plays major
// chords, the home row minor, the bottom row sevenths.
var keyRows = [harp.Kinds]string{
	harp.Major:   "qwertyuio",
	harp.Minor:   "asdfghjkl",
	harp.Seventh: "zxcvbnm,.",
}

// strumKeys are the strumplate's zones, low to high.
const strumKeys = "1234567890-="

// The number row must cover every zone.
var _ = [1]struct{}{}[len(strumKeys)-harp.Zones]
