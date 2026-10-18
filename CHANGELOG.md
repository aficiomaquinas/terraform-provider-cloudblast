## 0.2.1

BUG FIXES:

* cloudblast_security_group_attachment: honor the `retry_after` hint Cloudflare embeds in the problem+json BODY of 502 `origin_bad_gateway` responses (no `Retry-After` header is sent) — previously the hint was dropped and the backoff under-waited; retry cap raised 30s → 60s to match the provider's advertised "back off at least 60 seconds" (recurrence of the 2026-09-21 attach-502 outage, observed 2026-10-17 during the cortendesk staging germination)

## 0.2.0

FEATURES:

* cloudblast_server: optional ordered `failover_location_ids` — when the primary location returns NO_AVAILABLE_NODE, the create retries on the next location in the list; new computed `effective_location_id` reports where the server actually landed (#10)

ENHANCEMENTS:

* typed API error taxonomy: API failures are classified (capacity/plan/auth/rate-limit/server/validation/network) and surfaced as actionable provider diagnostics; raw payloads available via TF_LOG=DEBUG; `Retry-After` honored on 429 (#9)

## 0.1.0 (Unreleased)

FEATURES:
