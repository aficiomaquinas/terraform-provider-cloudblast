// Copyright 2026 aficiomaquinas
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// ErrorKind classifies CloudBlast API failures so resource code and users get
// actionable, error-specific guidance instead of a raw HTTP dump.
type ErrorKind string

const (
	// KindCapacity: the location has no nodes available for this plan (or the
	// plan is not offered in the location). Potentially recoverable by
	// choosing another location/plan.
	KindCapacity ErrorKind = "capacity"
	// KindPlan: the selected plan is out of stock everywhere.
	KindPlan ErrorKind = "plan"
	// KindAuth: token missing, invalid, or forbidden.
	KindAuth ErrorKind = "auth"
	// KindRateLimit: HTTP 429; retry later.
	KindRateLimit ErrorKind = "rate_limit"
	// KindServer: 5xx from the API; transient.
	KindServer ErrorKind = "server"
	// KindValidation: other 4xx; permanent, user must fix the request.
	KindValidation ErrorKind = "validation"
	// KindNetwork: request never reached the API (transport failure).
	KindNetwork ErrorKind = "network"
	// KindUnknown: anything that does not fit another kind.
	KindUnknown ErrorKind = "unknown"
)

// populateKind fills the optional Kind field of an APIError from its Code and
// Status. Code-based rules take precedence over status-based rules. The
// Error() string format is intentionally unaffected.
func populateKind(e *APIError) {
	if e == nil {
		return
	}
	switch e.Code {
	case "NO_AVAILABLE_NODE", "PLAN_NOT_AVAILABLE_IN_LOCATION":
		e.Kind = KindCapacity
		return
	case "PLAN_OUT_OF_STOCK":
		e.Kind = KindPlan
		return
	case "INVALID_TOKEN", "UNAUTHORIZED":
		e.Kind = KindAuth
		return
	}
	switch {
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		e.Kind = KindAuth
	case e.Status == http.StatusTooManyRequests:
		e.Kind = KindRateLimit
	case e.Status >= 500:
		e.Kind = KindServer
	case e.Status >= 400:
		e.Kind = KindValidation
	default:
		e.Kind = KindUnknown
	}
}

// parseRetryAfter parses a Retry-After header value, which may be
// delay-seconds ("120") or an HTTP-date. Returns nil when absent/unparseable
// or when an HTTP-date lies in the past.
func parseRetryAfter(v string) *time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		d := time.Duration(secs) * time.Second
		return &d
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return &d
		}
		return nil
	}
	return nil
}

// classifyErr maps an arbitrary error to an ErrorKind. APIErrors are
// recognised through wrapped error chains; transport failures ("request
// failed: ...") classify as KindNetwork.
func classifyErr(err error) ErrorKind {
	if err == nil {
		return KindUnknown
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		if apiErr.Kind == "" {
			populateKind(apiErr)
		}
		return apiErr.Kind
	}
	if strings.Contains(err.Error(), "request failed") {
		return KindNetwork
	}
	return KindUnknown
}

// kindDetail returns the actionable, kind-specific guidance paragraph.
func kindDetail(kind ErrorKind, apiErr *APIError) string {
	switch kind {
	case KindCapacity:
		return "The CloudBlast location has no available node for this plan right now (or the plan is not offered in this location). " +
			"Choose another location or plan, mark the location out of stock, or configure failover_location_ids to retry in fallback locations automatically."
	case KindPlan:
		return "The selected plan is out of stock. Pick a different plan (see the `cloudblast_plans` data source) or wait for restock; this error is permanent for this plan."
	case KindAuth:
		return "Authentication with the CloudBlast API failed. Check that CLOUDBLAST_API_TOKEN is set to a valid, non-revoked token."
	case KindRateLimit:
		if apiErr != nil && apiErr.RetryAfter != nil {
			return fmt.Sprintf("The CloudBlast API rate limit was hit. Retry after %s.", apiErr.RetryAfter)
		}
		return "The CloudBlast API rate limit was hit. Wait a moment and re-run the apply."
	case KindServer:
		return "The CloudBlast API returned a server-side error. This is transient — it is safe to re-run `terraform apply`."
	case KindValidation:
		return "The CloudBlast API rejected the request as invalid. This is a permanent error — verify plan_id, location_id and template against the data sources."
	case KindNetwork:
		return "The request never reached the CloudBlast API. Check network connectivity and DNS."
	default:
		return "The CloudBlast API request failed for an unrecognised reason."
	}
}

// AddAPIErrorDiagnostics adds a single error diagnostic whose detail contains
// kind-specific guidance followed by the raw API status, code and message.
// Non-API errors (network, wrapping) degrade gracefully.
func AddAPIErrorDiagnostics(diags *diag.Diagnostics, summary string, err error) {
	kind := classifyErr(err)

	var apiErr *APIError
	detail := kindDetail(kind, nil)
	if errors.As(err, &apiErr) {
		detail = kindDetail(kind, apiErr)
		detail += fmt.Sprintf("\n\nAPI status: %d\nError code: %s\nMessage: %s", apiErr.Status, apiErr.Code, apiErr.Message)
		if apiErr.RetryAfter != nil {
			detail += fmt.Sprintf("\nRetry after: %s", apiErr.RetryAfter)
		}
	} else if err != nil {
		detail += "\n\n" + err.Error()
	}

	diags.AddError(summary, detail)
}
