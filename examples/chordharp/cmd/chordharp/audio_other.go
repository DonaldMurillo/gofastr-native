//go:build !windows && (!darwin || !cgo)

package main

import "github.com/DonaldMurillo/gofastr-native/examples/chordharp/harp"

func openNative(*harp.Engine) (output, error) { return nil, errNoNative }
