package main

import (
	"errors"
	"strings"

	"github.com/DonaldMurillo/gofastr-native/internal/uitext"
)

func validateDeviceFeatures(a app, platform string) error {
	if a.Location || a.LocationUsage != "" {
		if a.Renderer != "native" {
			return errors.New("location flags require --renderer native")
		}
		if platform != "ios-sim" && platform != "ios-device" {
			return errors.New("--location is supported only on iOS")
		}
	}
	if !uitext.Plain(a.LocationUsage, 512) {
		return errors.New("--location-usage must be at most 512 UTF-8 bytes without control characters")
	}
	if a.LocationUsage != "" && !a.Location {
		return errors.New("--location-usage requires --location")
	}
	if a.Location && strings.TrimSpace(a.LocationUsage) == "" {
		return errors.New("iOS location requires --location-usage for when-in-use authorization")
	}
	if a.SignInWithApple && (a.Renderer != "native" || platform != "ios-sim" && platform != "ios-device") {
		return errors.New("--sign-in-with-apple requires the native iOS renderer")
	}
	if a.Push && (a.Renderer != "native" || platform != "ios-sim" && platform != "ios-device") {
		return errors.New("--push requires the iOS native renderer")
	}
	if (a.Calendar || a.ReceiveShares || a.SecureStorage || a.Widgets || a.BackgroundRefresh || a.AppGroup != "" || a.BiometricUsage != "") && a.Renderer != "native" {
		return errors.New("device feature flags require --renderer native")
	}
	if a.Calendar && (strings.TrimSpace(a.CalendarUsage) == "" || !uitext.Plain(a.CalendarUsage, 512)) {
		return errors.New("--calendar requires a plain nonempty --calendar-usage of at most 512 bytes")
	}
	if a.BackgroundRefresh && !strings.HasPrefix(platform, "ios") {
		return errors.New("--background-refresh is available on iOS only")
	}
	if (a.PushConfig == "") != (a.PushLibraries == "") {
		return errors.New("--push-config and --push-libraries are used together")
	}
	if a.PushConfig != "" && a.Renderer != "native" {
		return errors.New("push notifications require --renderer native")
	}
	if a.PushConfig != "" && strings.HasPrefix(platform, "ios") {
		return errors.New("--push-config and --push-libraries enable Android FCM push; iOS uses --push")
	}
	if a.Widgets && a.URLScheme == "" {
		return errors.New("--widgets requires --url-scheme for widget navigation")
	}
	if a.AppGroup != "" && (!strings.HasPrefix(a.AppGroup, "group.") || len(a.AppGroup) > 200 || !appIDRe.MatchString(a.AppGroup)) {
		return errors.New("--app-group must be a reverse-DNS identifier starting with group.")
	}
	if platform == "ios-sim" && (a.ReceiveShares || a.Widgets) && a.AppGroup == "" {
		return errors.New("iOS share and widget extensions require --app-group")
	}
	if !uitext.Plain(a.BiometricUsage, 512) {
		return errors.New("--biometric-usage must be at most 512 UTF-8 bytes without control characters")
	}
	if a.BiometricUsage != "" && !a.SecureStorage {
		return errors.New("--biometric-usage requires --secure-storage")
	}
	if platform == "ios-sim" && a.SecureStorage && a.BiometricUsage == "" {
		return errors.New("iOS secure storage requires --biometric-usage for Face ID authentication")
	}
	return nil
}
