# Spice Agent OpenAI Provider

`spice-agent-provider-openai` is the explicit OpenAI Responses adapter for
Spice Agent. It implements the exact `model.Provider` contract, owns no global
state, and keeps model selection in each `model.Request`.

Install the provider and the exact Spice compiler tool:

```text
go get github.com/spice-framework/spice-agent-provider-openai@<version>
go get -tool github.com/spice-framework/toolchain/cmd/spice@v0.1.0-preview.1
```

Construct it directly when the application wants explicit ownership:

```go
provider, err := openaiprovider.New(openaiprovider.Config{
	APIKey: os.Getenv("OPENAI_API_KEY"),
})
```

Construction validates configuration but performs no network request. The
provider never reads environment variables itself; the example deliberately
shows application-owned configuration. A request must choose its model through
`model.Request.Model`. A zero timeout selects two minutes; configured request
timeouts must be positive and no greater than thirty minutes.

Applications may opt into one replaceable fallback bean with an explicit blank
import:

```go
import _ "github.com/spice-framework/spice-agent-provider-openai/autoconfigure"
```

Importing the root package alone never activates a provider. There is no
registry, service locator, package scan, or hidden install step.

## Streaming contract

The adapter translates provider-neutral messages and function tools to the
Responses API and returns text deltas, finalized function calls, usage, and a
single completion event. Provider-hosted executable tools and unknown extension
parts fail closed. Output text and tool calls use the core operation limits.
The generated request sets `store=false`.

Failures before a stream exists are `model.ProviderError`; receive failures are
`model.StreamError`. Provider retries are bounded and permitted only before the
SDK has returned a stream. Spice never replays an observed or ambiguous stream.
All public failures are redacted. One goroutine may call `Recv` while another
calls `Close`; concurrent `Recv` calls are not supported.

Successful and failed terminal events can carry a small allowlisted metadata
record. To retain it in engine observations, composition must explicitly
allowlist `openaiprovider.MetadataNamespace` in `agent.EngineOptions`. The
record is limited to response/request IDs, model, status, service tier, and HTTP
status; it cannot contain prompts, results, tool arguments, headers, tokens,
keys, URLs, or raw provider objects.

Run the offline suite with `make verify`. The live acceptance is opt-in:

```text
SPICE_OPENAI_LIVE=1 OPENAI_API_KEY=<secret> OPENAI_MODEL=<model> go test -run TestLiveOpenAIResponse
```

See [the dependency review](docs/dependency-review.md),
[security review](docs/security-review.md), and [support matrix](docs/support.md).
