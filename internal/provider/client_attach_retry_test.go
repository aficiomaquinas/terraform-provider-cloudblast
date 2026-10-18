// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// attach502Body mirrors the real 502 origin_bad_gateway payload captured
// during the 2026-09-21 attach outage
// (SporeHarbor/docs/external-issues/2026-09-21_cloudblast-api-502-outage.md).
const attach502Body = `{"status":502,"error_name":"origin_bad_gateway","error_category":"origin",` +
	`"detail":"The origin web server returned an invalid or incomplete response to Cloudflare.",` +
	`"retryable":true,"retry_after":60,"zone":"console.cloudblast.io","cloudflare_error":true}`

// newAttachTestClient returns a Client pointed at baseURL with sub-millisecond
// retry timing and default retry counts so the retry loop runs fast in tests.
func newAttachTestClient(baseURL string) *Client {
	c := NewClient(baseURL, "test-token")
	c.attachRetryBaseWait = time.Millisecond
	c.attachRetryMaxWait = 4 * time.Millisecond
	return c
}

func TestAttachServerToGroupRetriesOn502ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/security-groups/349/servers/attach") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if body["server_uuid"] != "fb4727d5-4513-4e0a-afc0-4e217d4a9ec7" {
			t.Errorf("server_uuid = %q, want fb4727d5-4513-4e0a-afc0-4e217d4a9ec7", body["server_uuid"])
		}

		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(attach502Body))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"status":"attached"}}`))
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), 349, "fb4727d5-4513-4e0a-afc0-4e217d4a9ec7")
	if err != nil {
		t.Fatalf("AttachServerToGroup() = %v, want nil after 502-then-200 retry", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("server received %d calls, want 2 (initial + one retry)", got)
	}
}

func TestAttachServerToGroupPersistent5xxFailsAfterRetries(t *testing.T) {
	var attachCalls, verifyGets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Count attach POSTs only: after retry exhaustion the client now
		// performs the verify-after-502 read (GET the group), and that
		// read also 502s here — it must not be counted as an attach
		// attempt. The verification read itself is asserted below.
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/servers/attach") {
			attachCalls.Add(1)
		} else if r.Method == http.MethodGet {
			verifyGets.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(attach502Body))
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), 352, "11111111-2222-3333-4444-555555555555")
	if err == nil {
		t.Fatal("AttachServerToGroup() = nil, want error after persistent 502")
	}

	wantCalls := int32(1 + defaultAttachRetries)
	if got := attachCalls.Load(); got != wantCalls {
		t.Fatalf("server received %d attach calls, want %d (initial + %d retries)", got, wantCalls, defaultAttachRetries)
	}
	// The verify-after-502 read ran (and could not confirm membership
	// because this server 502s every request), so the original error
	// surfaced instead of a false success.
	if got := verifyGets.Load(); got == 0 {
		t.Error("server received no verification GET, want at least one verify-after-502 read")
	}

	// The error must stay in the repo's taxonomy: a KindServer APIError whose
	// message clearly states the retry exhaustion.
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not (wraps) an *APIError", err)
	}
	if apiErr.Status != http.StatusBadGateway {
		t.Errorf("APIError.Status = %d, want 502", apiErr.Status)
	}
	if apiErr.Kind != KindServer {
		t.Errorf("APIError.Kind = %q, want %q", apiErr.Kind, KindServer)
	}
	for _, want := range []string{"failed to attach server", "after 4 attempt(s)", "CloudBlast API error 502"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err.Error(), want)
		}
	}
}

func TestAttachServerToGroupDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":"FORBIDDEN","message":"token lacks scope"}}`))
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), 349, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	if err == nil {
		t.Fatal("AttachServerToGroup() = nil, want 403 error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server received %d calls, want 1 (4xx must not be retried)", got)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Kind != KindAuth {
		t.Errorf("error %v: want KindAuth APIError, got %v", err, apiErr)
	}
}

func TestAttachBackoffWait(t *testing.T) {
	base, maxWait := 2*time.Second, 30*time.Second
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{0, 2 * time.Second},
		{1, 4 * time.Second},
		{2, 8 * time.Second},
		{3, 16 * time.Second},
		{4, 30 * time.Second}, // capped
		{10, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := attachBackoffWait(tt.attempt, base, maxWait); got != tt.want {
			t.Errorf("attachBackoffWait(%d, %s, %s) = %s, want %s", tt.attempt, base, maxWait, got, tt.want)
		}
	}
}

// TestAttachParsesRetryAfterFromBody covers the 2026-10-17 cortendesk
// recurrence: Cloudflare's 502 origin_bad_gateway carries the retry hint in
// the problem+json BODY ({"retryable":true,"retry_after":60,...}) and sends NO
// Retry-After header. The client must surface it as APIError.RetryAfter so the
// attach retry loop can wait out the provider's advertised backoff.
func TestAttachParsesRetryAfterFromBody(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(attach502Body))
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	_, err := client.request(context.Background(), http.MethodPost,
		"/security-groups/386/servers/attach",
		map[string]string{"server_uuid": "ad980585-6c3d-4ab9-bacd-6be60b713f1a"}, nil)
	if err == nil {
		t.Fatal("request() = nil, want 502 APIError")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not (wraps) an *APIError", err)
	}
	if apiErr.RetryAfter == nil {
		t.Fatal("APIError.RetryAfter = nil, want 60s parsed from the problem+json body")
	}
	if got := *apiErr.RetryAfter; got != 60*time.Second {
		t.Errorf("APIError.RetryAfter = %s, want 60s", got)
	}
}

// TestBodyRetryAfterRejectsJunk pins bodyRetryAfter's negative paths.
func TestBodyRetryAfterRejectsJunk(t *testing.T) {
	if _, ok := bodyRetryAfter([]byte(`{"status":502}`)); ok {
		t.Error("bodyRetryAfter({status:502}) ok=true, want false (no hint)")
	}
	if _, ok := bodyRetryAfter([]byte(`not json`)); ok {
		t.Error("bodyRetryAfter(not json) ok=true, want false")
	}
	if secs, ok := bodyRetryAfter([]byte(`{"retry_after":-5}`)); ok {
		t.Errorf("bodyRetryAfter(negative) = %d,true, want false", secs)
	}
	if secs, ok := bodyRetryAfter([]byte(`{"retry_after":60}`)); !ok || secs != 60 {
		t.Errorf("bodyRetryAfter(60) = %d,%v, want 60,true", secs, ok)
	}
}
