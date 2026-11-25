package nativeui

import (
	"fmt"
	"testing"
)

func TestRendererContractRejectsSilentFallback(t *testing.T) {
	hello := Hello{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{
		CapabilityNavigation: SupportAvailable, CapabilityEditor: SupportAvailable, CapabilitySheets: SupportFallback,
	}}
	contract, err := Negotiate(hello)
	if err != nil {
		t.Fatal(err)
	}
	if err := contract.Check([]Requirement{{Capability: CapabilitySheets}}); err == nil {
		t.Fatal("native sheet requirement accepted a full-page fallback")
	}
	if err := contract.Check([]Requirement{{Capability: CapabilitySheets, AllowFallback: true}}); err != nil {
		t.Fatal(err)
	}
	hello.Capabilities[CapabilitySheets] = SupportAvailable
	if contract.Capabilities[CapabilitySheets] != SupportFallback {
		t.Fatal("advertisement mutation changed the connection contract")
	}
	for _, incompatible := range []Hello{
		{Minimum: 2, Maximum: 2, Renderer: "uikit"},
		{Minimum: 1, Maximum: 0, Renderer: "uikit"},
		{Minimum: 1, Maximum: 1, Renderer: "android-native"},
		{Minimum: 1, Maximum: 1, Renderer: "webview", Capabilities: map[Capability]Support{CapabilityNavigation: SupportFallback, CapabilityEditor: SupportAvailable}},
	} {
		if _, err := Negotiate(incompatible); err == nil {
			t.Fatalf("accepted incompatible peer: %+v", incompatible)
		}
	}
}

// TestNegotiateAcceptsEveryBuiltInCapability guards the hello bound: a native
// Android build with every opt-in flag advertises all built-in capabilities.
func TestNegotiateAcceptsEveryBuiltInCapability(t *testing.T) {
	all := map[Capability]Support{}
	for _, capability := range []Capability{
		CapabilityAnnouncement,
		CapabilityAudio,
		CapabilityBackgroundRefresh,
		CapabilityBrowser,
		CapabilityCalendar,
		CapabilityCamera,
		CapabilityClipboard,
		CapabilityContacts,
		CapabilityDeviceInfo,
		CapabilityDrafts,
		CapabilityEditor,
		CapabilityFiles,
		CapabilityHaptics,
		CapabilityIAP,
		CapabilityIncomingShares,
		CapabilityLive,
		CapabilityLocation,
		CapabilityNavigation,
		CapabilityNetwork,
		CapabilityNotifications,
		CapabilityOAuthWebLogin,
		CapabilityOpenURL,
		CapabilityPhotos,
		CapabilityPresentation,
		CapabilityPrivacy,
		CapabilityPush,
		CapabilityScanner,
		CapabilityAR,
		CapabilitySecureStorage,
		CapabilityShare,
		CapabilitySharing,
		CapabilitySheets,
		CapabilitySignInWithApple,
		CapabilitySpeechRecognition,
		CapabilityTextToSpeech,
		CapabilityWidgets,
	} {
		if !knownCapability(capability) {
			t.Fatalf("%s is not known", capability)
		}
		all[capability] = SupportAvailable
	}
	if _, err := Negotiate(Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: all}); err != nil {
		t.Fatal(err)
	}
	many := map[Capability]Support{}
	for i := 0; i <= maxHelloCapabilities; i++ {
		many[Capability(fmt.Sprintf("extension-%d/v1", i))] = SupportAvailable
	}
	if _, err := negotiate(Hello{Minimum: 1, Maximum: 1, Renderer: "android", Capabilities: many}, func(Capability) bool { return true }); err == nil {
		t.Fatal("oversized hello accepted")
	}
}
