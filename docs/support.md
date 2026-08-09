# Support and compatibility

| Contract | Supported value |
|---|---|
| Go | Exactly 1.26.5 for development and verification |
| Spice core | `v0.1.0-preview.1.0.20260806200749-524424a04df0` |
| Spice toolchain | `v0.1.0-preview.1.0.20260806203056-d0b9ac086bd6` |
| Spice Agent | `v0.0.0-20260806225954-af79fc7fe4ad` |
| OpenAI Go SDK | `v3.50.0` |
| Provider API | Exact `model.Provider` / `model.Stream` implementation |
| Activation | Direct `New` or explicit `/autoconfigure` blank import fallback |
| Operating systems | Windows, Linux, and macOS |
| Network | HTTPS OpenAI-compatible Responses endpoint, plus explicitly configured loopback-only HTTP for local test bridges; no construction-time network activity |
| Tests | Offline scripted/fake-TLS tests by default; explicitly enabled live acceptance |
| Request timeout | Two-minute default; positive values through thirty minutes |
| Stream concurrency | One `Recv` caller may race with `Close`; concurrent `Recv` calls unsupported |

Request model selection is always `model.Request.Model`. The adapter supports
provider-neutral text, function calls/results, function definitions, usage,
completion, and bounded extension metadata. Unknown message extensions and
provider-hosted executable output are rejected.

Pre-1.0 releases may revise the adapter contract. Compatibility is ordinary Go
module selection and is recorded in `spice-compatibility.json` and the starter
manifest.
