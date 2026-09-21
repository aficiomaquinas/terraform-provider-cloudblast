// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestPopulateKind(t *testing.T) {
	tests := []struct {
		name   string
		status int
		code   string
		want   ErrorKind
	}{
		{"capacity no available node", 409, "NO_AVAILABLE_NODE", KindCapacity},
		{"capacity plan not in location", 400, "PLAN_NOT_AVAILABLE_IN_LOCATION", KindCapacity},
		{"plan out of stock", 409, "PLAN_OUT_OF_STOCK", KindPlan},
		{"auth invalid token", 401, "INVALID_TOKEN", KindAuth},
		{"auth unauthorized code", 403, "UNAUTHORIZED", KindAuth},
		{"auth bare 401", 401, "Internal Error", KindAuth},
		{"auth bare 403", 403, "Forbidden", KindAuth},
		{"rate limit", 429, "Too Many Requests", KindRateLimit},
		{"server 500", 500, "Internal Server Error", KindServer},
		{"server 503", 503, "Service Unavailable", KindServer},
		{"validation 400", 400, "Bad Request", KindValidation},
		{"validation 404", 404, "Not Found", KindValidation},
		{"validation 422", 422, "Unprocessable Entity", KindValidation},
		{"unknown 2xx with error body", 200, "Weird", KindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &APIError{Status: tt.status, Code: tt.code}
			populateKind(e)
			if e.Kind != tt.want {
				t.Fatalf("populateKind(status=%d, code=%q) = %q, want %q", tt.status, tt.code, e.Kind, tt.want)
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantNil bool
		wantMin time.Duration // lower bound when non-nil (HTTP-date path is time-dependent)
	}{
		{"empty", "", true, 0},
		{"seconds", "120", false, 120 * time.Second},
		{"zero seconds", "0", false, 0},
		{"negative treated invalid", "-5", true, 0},
		{"garbage", "soon", true, 0},
		{"http date in past", "Mon, 02 Jan 2006 15:04:05 GMT", true, 0},
		{"http date in future", time.Now().UTC().Add(2 * time.Minute).Format("Mon, 02 Jan 2006 15:04:05 GMT"), false, time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRetryAfter(tt.header)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("parseRetryAfter(%q) = %v, want nil", tt.header, *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseRetryAfter(%q) = nil, want non-nil", tt.header)
			}
			if *got < tt.wantMin {
				t.Fatalf("parseRetryAfter(%q) = %v, want >= %v", tt.header, *got, tt.wantMin)
			}
		})
	}
}

func TestClassifyErr(t *testing.T) {
	networkErr := fmt.Errorf("request failed: %w", errors.New("connection refused"))

	tests := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"nil", nil, KindUnknown},
		{"api capacity", &APIError{Status: 409, Code: "NO_AVAILABLE_NODE"}, KindCapacity},
		{"api plan", &APIError{Status: 409, Code: "PLAN_OUT_OF_STOCK"}, KindPlan},
		{"api auth", &APIError{Status: 401, Code: "INVALID_TOKEN"}, KindAuth},
		{"api rate limit", &APIError{Status: 429, Code: "Too Many Requests"}, KindRateLimit},
		{"api server", &APIError{Status: 502, Code: "Bad Gateway"}, KindServer},
		{"api validation", &APIError{Status: 404, Code: "Not Found"}, KindValidation},
		{"wrapped api error", fmt.Errorf("create failed: %w", &APIError{Status: 409, Code: "NO_AVAILABLE_NODE"}), KindCapacity},
		{"network", networkErr, KindNetwork},
		{"wrapped network", fmt.Errorf("outer: %w", networkErr), KindNetwork}, // Error() string still contains "request failed"
		{"plain error", errors.New("something exploded"), KindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyErr(tt.err); got != tt.want {
				t.Fatalf("classifyErr(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestClassifyErrFillsEmptyKind(t *testing.T) {
	e := &APIError{Status: 429, Code: "Too Many Requests"}
	if got := classifyErr(e); got != KindRateLimit {
		t.Fatalf("classifyErr = %q, want %q", got, KindRateLimit)
	}
	if e.Kind != KindRateLimit {
		t.Fatalf("expected Kind populated on the error itself, got %q", e.Kind)
	}
}

func TestErrorFormatUnchanged(t *testing.T) {
	e := &APIError{Status: 409, Code: "NO_AVAILABLE_NODE", Message: "no nodes"}
	populateKind(e)
	want := "CloudBlast API error 409 (NO_AVAILABLE_NODE): no nodes"
	if got := e.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestAddAPIErrorDiagnostics(t *testing.T) {
	t.Run("capacity error includes guidance and raw fields", func(t *testing.T) {
		var d diag.Diagnostics
		AddAPIErrorDiagnostics(&d, "Failed to create server", &APIError{
			Status: 409, Code: "NO_AVAILABLE_NODE", Message: "no free nodes",
		})
		if !d.HasError() {
			t.Fatal("expected error diagnostic")
		}
		if d.Errors()[0].Summary() != "Failed to create server" {
			t.Fatalf("summary = %q", d.Errors()[0].Summary())
		}
		detail := d.Errors()[0].Detail()
		for _, want := range []string{"failover_location_ids", "API status: 409", "Error code: NO_AVAILABLE_NODE", "Message: no free nodes"} {
			if !contains(detail, want) {
				t.Fatalf("detail missing %q:\n%s", want, detail)
			}
		}
	})

	t.Run("rate limit includes retry hint", func(t *testing.T) {
		d := &diag.Diagnostics{}
		ra := 30 * time.Second
		AddAPIErrorDiagnostics(d, "Failed to create server", &APIError{
			Status: 429, Code: "Too Many Requests", Message: "slow down", RetryAfter: &ra,
		})
		detail := d.Errors()[0].Detail()
		for _, want := range []string{"Retry after 30s", "API status: 429"} {
			if !contains(detail, want) {
				t.Fatalf("detail missing %q:\n%s", want, detail)
			}
		}
	})

	t.Run("network error degrades gracefully", func(t *testing.T) {
		var d diag.Diagnostics
		AddAPIErrorDiagnostics(&d, "Failed to create server", fmt.Errorf("request failed: connection refused"))
		detail := d.Errors()[0].Detail()
		if !contains(detail, "network connectivity") || !contains(detail, "connection refused") {
			t.Fatalf("unexpected detail:\n%s", detail)
		}
	})
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
