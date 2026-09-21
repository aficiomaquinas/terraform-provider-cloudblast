## 0.2.0

FEATURES:

* cloudblast_server: optional ordered `failover_location_ids` — when the primary location returns NO_AVAILABLE_NODE, the create retries on the next location in the list; new computed `effective_location_id` reports where the server actually landed (#10)

ENHANCEMENTS:

* typed API error taxonomy: API failures are classified (capacity/plan/auth/rate-limit/server/validation/network) and surfaced as actionable provider diagnostics; raw payloads available via TF_LOG=DEBUG; `Retry-After` honored on 429 (#9)

## 0.1.0 (Unreleased)

FEATURES:
