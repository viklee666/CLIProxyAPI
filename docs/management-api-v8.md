# Fork management API and routing

The upstream integrations from November 23–24, 2026 have been reverted to the
pre-merge v7 baseline. The later fork features remain: request monitoring,
Pelican/Cockpit comparisons, health-gated priority routing, stored-credential
failover, and healthy session affinity across priority changes.

Only the fork comparison API retains its `/v8/management/requests/pelican` address
(GET and POST). This does not restore the upstream v8 configuration schema,
SIWC binding, or duplex WebSocket implementation. Existing configuration,
credential-file import/paste, and other management operations use the pre-merge
contracts under `/v0/management`.

Management requests require `Authorization: Bearer <management-key>` or
`X-Management-Key: <management-key>` and follow the configured remote-access policy.

## Strict priority tiers and health-gated fallback

Routing compares every eligible provider credential and credential file in one descending priority order for the requested model. Provider and file credentials participate as peers after model aliases, model capabilities, client-group access, and ownership checks are resolved. An existing healthy, authorized session binding takes precedence over priority: changing priorities or recovering a higher tier does not move that session. New sessions and necessary replacements use the highest eligible tier, with round-robin, fill-first, weighted round-robin, or adaptive routing operating inside that tier. Session reuse still respects model support, disabled states, credential availability, health, and configured concurrency limits.

A lower tier becomes eligible when every eligible target in the preferred tier is unhealthy or has reached its configured concurrency limit. Health uses completed upstream results scoped by credential, requested model, and client-access partition. The sixth consecutive upstream failure demotes a target. A trailing 60-second error rate must exceed 60 percent across at least 10 completed samples before it demotes a target. Five consecutive failures and exactly 60 percent errors remain eligible. Busy targets below their positive concurrency limit remain eligible. A zero or omitted concurrency limit means unlimited. RPM saturation does not demote a tier. Busy and time-to-first-token observations alone do not demote a target.

The rolling error-rate window expires after 60 seconds without matching samples. Consecutive failures are tracked separately across idle periods and are cleared by a successful completed response, not by window expiration. A streak-triggered demotion isolates the target for 60 seconds before allowing a recovery probe; another failure isolates it again. Higher tiers become eligible again after recovery, without displacing healthy existing session bindings. Keys with the same authorized group set share health; different client-group partitions do not demote one another. Administrative keys evaluate aggregate target outcomes and the complete eligible target set without inheriting a group-specific priority override. Existing tenant ownership checks still apply.

Configured provider entries apply cooldown and attempted-ID filtering after the tier gate: one failure, a cooldown, or an exhausted retry list alone does not unlock a lower healthy provider tier. Stored credential files (including git/object-store/Postgres-backed credentials) exclude unavailable targets before choosing the highest remaining tier. After a retryable credential failure, execution sweeps the remaining authorized, same-model stored credentials in priority order, without the `max-retry-credentials` cap ending that sweep early. Each identity is tried at most once in the sweep (normal same-identity authentication repair remains allowed); exhaustion returns the upstream error without restarting a retry round. Configured AI-provider keys do not join this credential-only sweep. Client-group and tenant restrictions, model support, cooldowns, concurrency/RPM admission, and zero-weight exclusions remain enforced. RPM alone never opens a lower tier. Explicit pinned credentials, disabled retries, request-invalid/stop rules, and cancellation remain terminal.

Codex stored credentials automatically withhold bounded stream bootstrap events so quota, rate-limit, and overload errors before semantic output can trigger internal failover. Once content or a tool operation is accepted, execution cannot be replayed, even if translation emits no downstream bytes for that event. An error after that boundary is returned in-stream rather than duplicating generation. Configured provider keys retain the explicit bootstrap-buffering setting.

## Pelican generation comparison

`GET` and `POST /v8/management/requests/pelican` require management authentication. The manager extension forwards only this explicit v8 path using its saved CPA management key. `GET` resolves one exact runtime target and returns its `auth_id`, `auth_index`, `name`, and published `models`. For configured providers, supply `provider_kind` and the zero-based `config_index`; OpenAI-compatible key entries also use `key_index`. Supported kinds are `gemini`, `interactions`, `codex`, `claude`, `vertex`, `openai`, `xai`, and `meta`. Credential discovery accepts `auth_id` or the file `name` and `auth_index`.

```json
{"auth_id":"exact-runtime-identity","model":"configured-model-alias","prompt":"Create an animated inline SVG of a pelican riding a bicycle.","effort":"high"}
```

Prefer the discovered `auth_id` for generation. Any accompanying `name` or `auth_index` must match that identity. Name/index-only requests must resolve uniquely; ambiguous targets are rejected rather than choosing a different configuration with the same key. `model` and `prompt` are required. Optional `effort` defaults to `medium` and accepts `low`, `medium`, `high`, or `xhigh`. The request body is limited to 128 KiB, the prompt to 100,000 bytes, and the model to 512 bytes. Disabled or sign-in-required targets are rejected.

The normal executor pipeline performs model translation, credential refresh, proxy handling, and final user payload rules. Each generation uses an isolated session and a pinned local runtime identity, including when Home routing is enabled. Ordinary retries and credential failover are disabled, while a recoverable OAuth 401 may refresh and retry the same identity. Disconnecting or cancelling cancels the generation. Calls consume the selected account's upstream quota.

Success returns `auth_id`, `auth_index`, `name`, the response `model`, complete `answer`, `duration_ms`, and `usage`. Empty, incomplete, truncated, or tool-only responses are failures. Upstream authentication failures return HTTP 502 with `upstream_status`, rather than invalidating the operator's management login. Management authentication failures still return HTTP 401.

The UI supports individual targets, multiple selection, and selecting all matching targets. The pelican animation, candy reasoning question, and custom-prompt modes follow [Cockpit Tools](https://github.com/jlcodes99/cockpit-tools/tree/d4b7de6697efc4bc251962e56d71ff6ffa05e956/src/components/codex/pelican). Preset questions are identical across targets; the candy reference is displayed only in the UI and is never appended to a generation request. Custom prompts can request other subjects or plain-text tasks. Complete generated HTML is previewed in an opaque-origin sandbox with inline scripts allowed for animation, but without same-origin privileges or external network access; the unmodified answer remains available as escaped source. These tasks support manual comparison, not a calibrated IQ score or a guaranteed-quality benchmark.

