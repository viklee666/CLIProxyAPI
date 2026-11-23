# AI Providers

AI Providers controls how CPA forwards client requests to upstream model services. These settings are provider credentials and routing settings, not CPAMP login or the CPA Management Key.

Use [Configuration](./configuration.md) for Manager Server connection, collector, and runtime switches.

## Supported Configuration Types

Provider entries are grouped by type. Common types include:

- Codex
- Claude Code
- Gemini / AI Studio
- Vertex
- OpenAI-compatible

Each provider has different fields, but the core questions are the same: where requests go, which credentials are used, which models are available, whether the entry is enabled, and whether headers or compatibility APIs need special handling.

## Add Or Edit A Provider

1. Click add configuration and choose the provider type.
2. Fill name, Base URL, API key, auth file, or OAuth-related fields.
3. If the page asks for `auth_index`, use a stable and unique value so usage, quota, and inspection can connect data to the same account.
4. Configure model lists or model rules. Keep client model names, provider model names, and model-price names clear.
5. Save, then send one low-cost request.
6. Open [Monitoring](./monitoring.md) and confirm provider, model, account, and status.

## Field Guidance

- **Base URL**: should point to the upstream model service or CPA-compatible endpoint, not the CPAMP docs or panel URL.
- **API Key**: a provider or client credential. Do not use CPA Management Key or CPAMP Admin Key here.
- **Auth file**: use it for OAuth or account-file providers. Maintain account state in [Auth Files](./auth-files.md).
- **Model list**: affects visible models and routing. Cost estimation also needs matching names in [Model Prices](./model-prices.md).
- **Enabled state**: disabled entries should no longer receive new routed requests.

## Request Limits

Every configured provider and auth-file credential supports a concurrent request limit and an RPM limit. Both default to `0` (unlimited); positive integers enable a limit. Provider YAML uses `concurrency-limit` and `rpm-limit`; auth files use `concurrency_limit` and `rpm_limit`.

Open a provider's editor or an auth file's field editor to change these values. Existing xAI and Meta API-key records have a dedicated limits section on AI Providers. OpenAI-compatible providers share their limits across all API keys in the same provider record. Each other provider record or auth-file credential has its own budget.

Concurrency remains occupied until a response finishes, including the full lifetime of a stream. RPM uses a rolling 60-second window. When an entry has reached its limit, CPA can select another eligible entry before sending an upstream request. If no entry can admit the request, CPA returns HTTP 429 without cooling down the credentials. Counters are local to the running CPA process and reset on restart.

## Custom Upstream Error Handling

The OpenAI-compatible provider editor offers an **Add error handling rule** button. For each rule, enter an HTTP status, one or more error-text fragments or regular expressions (one per line), and choose the handling action. You can add and remove rules directly; no JSON syntax is required. Existing saved rules are loaded into the form. Rules are evaluated in order, and a rule matches when its status and any text or regex pattern match.

The four actions choose whether to stop or continue credential selection and whether to apply cooldown. Configured AI Providers still follow the single-upstream-request policy described below; a continue action does not authorize replay after an upstream request was sent.

## Request Counts And Upstream Errors

Services configured on AI Providers (`*-api-key` and `openai-compatibility`, including keyless compatible endpoints) send at most one upstream inference request per client request. An upstream error, empty stream, or first-event timeout ends the request and returns the error. CPA does not automatically switch credentials or models, replay the request, or follow HTTP redirects.

Global and provider `request-retry`, `max-retry-credentials`, and streaming retry settings cannot increase these providers' call counts. Separate client requests still select providers according to routing policy: ten routable client requests produce ten upstream requests. OAuth/auth-file accounts retain their own retry policies.

## Verify After Saving

The reliable test is not the "saved" toast. Send a real request:

1. Ask a client to use a low-cost model.
2. In [Monitoring](./monitoring.md), confirm provider, model, account, and status code.
3. If the request succeeds but cost is empty, check [Model Prices](./model-prices.md).
4. If the request fails like an auth issue, check [Auth Files](./auth-files.md) or [OAuth Login](./oauth.md).
5. If no request event appears, troubleshoot the monitoring collection path.

## Common Problems

- **Model still unavailable after save**: confirm the entry is enabled, the model name matches, and client traffic passes through CPA.
- **Requests hit the wrong provider**: check routing rules, model aliases, and provider order.
- **Only some accounts fail**: use Auth Files or Codex Inspection and search by `auth_index`.
- **Cost estimate is wrong**: provider configuration does not set prices. Maintain the matching model in Model Prices.

