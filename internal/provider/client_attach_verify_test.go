// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Verify-after-502 tests. The CloudBlast attach handler has been observed to
// COMMIT the security-group membership and then die before writing the
// response (Cloudflare origin_bad_gateway 502): the write is live, only the
// ack was lost
// (SporeHarbor/docs/external-issues/2026-10-18_cloudblast-attach-502-formal-reproduction.md).
// AttachServerToGroup must therefore read the group back when the retry loop
// is exhausted on a 5xx and turn a confirmed membership into success.

const (
	verifyTestGroupID   = 352
	verifyTestServerID  = "11111111-2222-3333-4444-555555555555"
	verifyTestSGPayload = `{"data":{"id":352,"name":"cortendesk-staging-2610180346-","vm_count":1,` +
		`"servers":[{"uuid":"11111111-2222-3333-4444-555555555555","uuid_short":"11111111","name":"VMA21"}]}}`
	verifyTestSGEmptyPayload = `{"data":{"id":352,"name":"cortendesk-staging-2610180346-","vm_count":0,` +
		`"servers":[]}}`
)

// TestAttachServerToGroupVerifiesAfter502WhenAttachCommitted covers case (a):
// every attach POST 502s, the verification read shows the server IS in the
// group's servers[] (the write committed, the ack was lost) => success.
func TestAttachServerToGroupVerifiesAfter502WhenAttachCommitted(t *testing.T) {
	var attachCalls, verifyGets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/security-groups/352" {
			verifyGets.Add(1)
			_, _ = w.Write([]byte(verifyTestSGPayload))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/security-groups/352/servers/attach") {
			attachCalls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(attach502Body))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), verifyTestGroupID, verifyTestServerID)
	if err != nil {
		t.Fatalf("AttachServerToGroup() = %v, want nil: attach committed server-side despite the 502s", err)
	}
	if got, want := attachCalls.Load(), int32(1+defaultAttachRetries); got != want {
		t.Errorf("server received %d attach calls, want %d (initial + %d retries)", got, want, defaultAttachRetries)
	}
	if got := verifyGets.Load(); got != 1 {
		t.Errorf("server received %d verification GET(s), want exactly 1 (confirmed membership ends the read)", got)
	}
}

// TestAttachServerToGroup502VerificationNotAttachedFails covers case (b):
// every attach POST 502s AND the verification read shows the server is NOT in
// the group => the original retry-exhaustion error must surface, unchanged in
// shape (wrapped 502 KindServer APIError).
func TestAttachServerToGroup502VerificationNotAttachedFails(t *testing.T) {
	var attachCalls, verifyGets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/security-groups/352" {
			verifyGets.Add(1)
			_, _ = w.Write([]byte(verifyTestSGEmptyPayload))
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/security-groups/352/servers/attach") {
			attachCalls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(attach502Body))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), verifyTestGroupID, verifyTestServerID)
	if err == nil {
		t.Fatal("AttachServerToGroup() = nil, want error: verification read shows the server NOT attached")
	}
	if got, want := attachCalls.Load(), int32(1+defaultAttachRetries); got != want {
		t.Errorf("server received %d attach calls, want %d", got, want)
	}
	if got := verifyGets.Load(); got != 1 {
		t.Errorf("server received %d verification GET(s), want exactly 1 (definitive absence needs no read retry)", got)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not (wraps) an *APIError", err)
	}
	if apiErr.Status != http.StatusBadGateway || apiErr.Kind != KindServer {
		t.Errorf("APIError = status %d kind %q, want 502 %q", apiErr.Status, apiErr.Kind, KindServer)
	}
	for _, want := range []string{"failed to attach server", "after 4 attempt(s)", "CloudBlast API error 502"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err.Error(), want)
		}
	}
}

// TestAttachServerToGroupSuccessSkipsVerificationRead covers case (c): a 200
// attach returns immediately and MUST NOT issue any verification GET — the
// read is an anomaly probe for lost acks, not part of the happy path.
func TestAttachServerToGroupSuccessSkipsVerificationRead(t *testing.T) {
	var attachCalls, verifyGets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			verifyGets.Add(1)
			t.Errorf("unexpected verification GET on the success path: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/security-groups/352/servers/attach") {
			attachCalls.Add(1)
			_, _ = w.Write([]byte(`{"data":{"status":"attached"}}`))
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := newAttachTestClient(srv.URL)
	err := client.AttachServerToGroup(context.Background(), verifyTestGroupID, verifyTestServerID)
	if err != nil {
		t.Fatalf("AttachServerToGroup() = %v, want nil on a plain 200", err)
	}
	if got := attachCalls.Load(); got != 1 {
		t.Errorf("server received %d attach calls, want 1 (no retry on success)", got)
	}
	if got := verifyGets.Load(); got != 0 {
		t.Errorf("server received %d verification GET(s), want 0", got)
	}
}
