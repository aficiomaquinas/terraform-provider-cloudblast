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
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
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
	if got := calls.Load(); got != wantCalls {
		t.Fatalf("server received %d calls, want %d (initial + %d retries)", got, wantCalls, defaultAttachRetries)
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
	base, max := 2*time.Second, 30*time.Second
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
		if got := attachBackoffWait(tt.attempt, base, max); got != tt.want {
			t.Errorf("attachBackoffWait(%d, %s, %s) = %s, want %s", tt.attempt, base, max, got, tt.want)
		}
	}
}
