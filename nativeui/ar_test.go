package nativeui

import (
	"context"
	"math"
	"testing"

	"github.com/DonaldMurillo/gofastr-native/device/ar"
	"github.com/DonaldMurillo/gofastr-native/device/sharing"
)

func TestARServiceIsNotAdmittedOutsideAndroid(t *testing.T) {
	for _, renderer := range []string{"uikit", "webview"} {
		t.Run(renderer, func(t *testing.T) {
			definition := &ServiceRequest{ID: "device", AR: &ar.Request{Operation: ar.Check}}
			runtime, command := serviceFixtureForRenderer(t, definition, func(context.Context, Request, ServiceResult) (Result, error) {
				t.Error("unsupported renderer invoked AR continuation")
				return Result{}, nil
			}, nil, renderer)
			command.Kind, command.RequestID, command.Action = "action", "start", "start"
			if reply := serviceCommand(t, context.Background(), runtime, command); reply.Error != nil {
				t.Fatal(reply.Error)
			}
			command.Kind = "complete"
			reply := serviceCommand(t, context.Background(), runtime, command)
			if reply.Error == nil || reply.Error.Code != "unsupported" || reply.ServiceID != "" {
				t.Fatalf("AR task was not refused: %+v", reply)
			}
		})
	}
}

func TestARService(t *testing.T) {
	check := ServiceRequest{ID: "check", AR: &ar.Request{Operation: ar.Check}}
	place := ServiceRequest{ID: "place", AR: &ar.Request{Operation: ar.Place}}
	for _, req := range []ServiceRequest{check, place} {
		if err := req.Validate(); err != nil || req.capability() != CapabilityAR {
			t.Fatalf("AR request %+v: %v", req, err)
		}
		if err := req.accept(ServiceResult{Status: EffectCancelled}); err != nil {
			t.Fatal(err)
		}
		if req.accept(ServiceResult{Status: EffectCompleted}) == nil {
			t.Fatal("accepted missing AR result")
		}
	}
	for _, availability := range []ar.Availability{ar.Ready, ar.InstallNeeded, ar.UpdateNeeded, ar.Unsupported, ar.Unknown} {
		if err := check.accept(ServiceResult{Status: EffectCompleted, AR: &ar.Result{Availability: availability}}); err != nil {
			t.Fatal(err)
		}
	}
	anchor := &ar.Result{Position: []float64{0, 1.25, -2}}
	if err := place.accept(ServiceResult{Status: EffectCompleted, AR: anchor}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		req ServiceRequest
		out ServiceResult
	}{
		{check, ServiceResult{Status: EffectCompleted, AR: anchor}},
		{check, ServiceResult{Status: EffectCompleted, AR: &ar.Result{Availability: "invalid"}}},
		{place, ServiceResult{Status: EffectCancelled, AR: anchor}},
		{place, ServiceResult{Status: EffectCompleted, AR: &ar.Result{Position: []float64{1, 2}}}},
		{place, ServiceResult{Status: EffectCompleted, AR: &ar.Result{Position: []float64{1, 2, math.Inf(1)}}}},
		{place, ServiceResult{Status: EffectCompleted, AR: &ar.Result{Position: []float64{1, 2, 3}, Availability: ar.Ready}}},
		{ServiceRequest{ID: "copy", Clipboard: &sharing.Clipboard{Text: "x"}}, ServiceResult{Status: EffectCompleted, AR: anchor}},
	} {
		if tc.req.accept(tc.out) == nil {
			t.Errorf("accepted invalid outcome %+v for %+v", tc.out, tc.req)
		}
	}
	check.Clipboard = &sharing.Clipboard{Text: "x"}
	if check.Validate() == nil || (&ar.Request{}).Validate() == nil {
		t.Fatal("accepted an invalid operation")
	}
}
