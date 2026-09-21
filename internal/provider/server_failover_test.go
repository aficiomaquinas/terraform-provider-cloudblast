// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestBuildCreateAttempts(t *testing.T) {
	tests := []struct {
		name        string
		primary     int64
		failoverIDs []int64
		want        []int
	}{
		{
			name:        "no failover configured",
			primary:     4,
			failoverIDs: nil,
			want:        []int{4},
		},
		{
			name:        "ordered fallbacks preserved",
			primary:     4,
			failoverIDs: []int64{1, 2, 3},
			want:        []int{4, 1, 2, 3},
		},
		{
			name:        "primary in failover list is dropped",
			primary:     4,
			failoverIDs: []int64{4, 1},
			want:        []int{4, 1},
		},
		{
			name:        "duplicate fallbacks deduped keeping first occurrence",
			primary:     4,
			failoverIDs: []int64{2, 3, 2, 1, 3},
			want:        []int{4, 2, 3, 1},
		},
		{
			name:        "only primary configured yields single attempt",
			primary:     7,
			failoverIDs: []int64{7, 7},
			want:        []int{7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCreateAttempts(tt.primary, tt.failoverIDs)
			if len(got) != len(tt.want) {
				t.Fatalf("buildCreateAttempts(%d, %v) = %v, want %v", tt.primary, tt.failoverIDs, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("buildCreateAttempts(%d, %v) = %v, want %v", tt.primary, tt.failoverIDs, got, tt.want)
				}
			}
		})
	}
}

func TestShouldFailOver(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "NO_AVAILABLE_NODE is retried",
			err:  &APIError{Status: 409, Code: "NO_AVAILABLE_NODE", Message: "no free nodes"},
			want: true,
		},
		{
			name: "wrapped NO_AVAILABLE_NODE is retried",
			err:  fmt.Errorf("create failed: %w", &APIError{Status: 409, Code: "NO_AVAILABLE_NODE"}),
			want: true,
		},
		{
			name: "PLAN_OUT_OF_STOCK is terminal",
			err:  &APIError{Status: 409, Code: "PLAN_OUT_OF_STOCK", Message: "plan exhausted"},
			want: false,
		},
		{
			name: "PLAN_NOT_AVAILABLE_IN_LOCATION is terminal for failover",
			err:  &APIError{Status: 400, Code: "PLAN_NOT_AVAILABLE_IN_LOCATION"},
			want: false,
		},
		{
			name: "auth error is terminal",
			err:  &APIError{Status: 401, Code: "INVALID_TOKEN"},
			want: false,
		},
		{
			name: "validation error is terminal",
			err:  &APIError{Status: 422, Code: "Unprocessable Entity"},
			want: false,
		},
		{
			name: "5xx is terminal",
			err:  &APIError{Status: 503, Code: "Service Unavailable"},
			want: false,
		},
		{
			name: "network error is terminal",
			err:  fmt.Errorf("request failed: connection refused"),
			want: false,
		},
		{
			name: "plain error is terminal",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil is terminal",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldFailOver(tt.err); got != tt.want {
				t.Fatalf("shouldFailOver(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
