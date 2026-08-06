# Dependency review: OpenAI Go

## Decision

Approved for this isolated provider module at exactly
`github.com/openai/openai-go/v3 v3.50.0`. No other third-party runtime module is
directly selected.

## Maintenance and license

The package is OpenAI's official Go SDK, generated and maintained from the API
schema. Version 3.50.0 was selected for the architecture proof and requires Go
1.25 or newer. The upstream license is Apache-2.0 and is retained in committed
vendor contents. Version changes require an explicit API, license, vulnerability,
and generated-dependency review.

## Security and privacy

- The adapter constructs `responses.ResponseService` directly with explicit
  options; it does not use `openai.NewClient` and therefore does not read ambient
  `OPENAI_*` variables.
- API keys are required typed configuration and are marked secret for Spice
  metadata. Public errors and diagnostic strings never contain the key.
- Only absolute HTTPS endpoints without URL user information are accepted.
- Prompt, output, tool arguments, authorization headers, and raw provider bodies
  are not general-purpose observability metadata.
- Construction performs no network operation and creates no background goroutine.

## Cancellation, retry, and ownership

Every eventual Responses operation is caller-context-owned. The provider owns
one SDK service and HTTP client per constructed bean. Timeouts and retry counts
are bounded. Phase 3 will disable replay once a stream is observed and will map
partial-stream failure into an explicit terminal event.

## Observability

The planned adapter emits bounded operation identity, model identity, provider
request ID, usage totals, duration, and typed outcome. Content and secrets are
excluded unless an application deliberately installs a content recorder outside
this module.

## Build-only dependencies

Spice core and toolchain are pinned at `v0.1.0-preview.1`. The toolchain is
authorized through the standard Go `tool` directive. It participates in normal
module selection and vendor verification; there is no custom dependency system.
