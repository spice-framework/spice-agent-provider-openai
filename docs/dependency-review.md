# Dependency review

## Decision

Approved runtime dependencies are pinned to:

- `github.com/spice-framework/spice-agent v0.1.0-preview.4`
- `github.com/openai/openai-go/v3 v3.50.0`

The first supplies the exact provider-neutral contracts and bounds. The second
is OpenAI's official generated Go SDK for the Responses API. Both are
Apache-2.0. Licenses are retained in committed vendor contents. Version changes
require API, maintenance, license, vulnerability, cancellation, retry, and
generated-dependency review.

## Ownership and dependency direction

This adapter depends on the agent SPI; the agent kernel never depends on this
provider. It constructs an instance-owned `responses.ResponseService` with
explicit options and exposes only `model.Provider`. No raw SDK client, response,
or stream crosses the public boundary.

Spice core is pinned at `v0.1.0-preview.2`, and the toolchain is pinned at
`v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6`. The compiler is authorized by
the standard Go `tool` directive. All runtime and build modules participate in
ordinary Go selection, checksums, vendoring, and offline verification; there is
no custom dependency system.

## Security and privacy

- The provider does not call `openai.NewClient`, so it does not capture ambient
  `OPENAI_*` variables.
- API keys arrive only through explicit typed configuration and are secret
  marked. Configuration and error strings redact them.
- Remote endpoints require absolute HTTPS URLs without URL user information.
  Plain HTTP is accepted only for exact `localhost` or a parsed loopback IP
  literal to support explicit local test bridges; lookalike and wildcard hosts
  are rejected.
- Prompt/output content, tool arguments/results, headers, URLs, credentials,
  raw response bodies, and raw SDK objects are excluded from metadata.
- Construction performs no network operation and starts no goroutine.

## Cancellation and retry

Each operation derives from the caller context. After synchronously acquiring
the first response event, one bounded pump owns the SDK stream; `Close` cancels
and joins it. A separate `Recv` deadline does not create an unbounded helper
goroutine. Configuration accepts request timeouts from greater than zero through
thirty minutes; zero selects the two-minute default. One receiver may race with
`Close` safely. Application-supplied transports remain trusted cooperative code
and must honor context cancellation or close.

The SDK retry count is explicit and bounded to 0–8. A stable hashed idempotency
key is supplied for startup attempts. The adapter never retries after returning
a stream, and the engine does not retry after observing an event. Ambiguous or
partial streams are therefore never replayed by this module.

## Observability

Provider metadata is optional, bounded, namespaced, and application-allowlisted.
The only permitted fields are response ID, request ID, model, status, service
tier, and HTTP status. Usage remains provider-neutral core data. Live testing is
opt-in and never prints prompts, outputs, keys, authorization headers, or raw
provider errors.
