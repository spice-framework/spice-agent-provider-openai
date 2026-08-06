# Support and compatibility

| Contract | Supported value |
|---|---|
| Go | Exactly 1.26.5 for development and verification |
| Spice core | `v0.1.0-preview.1` |
| Spice toolchain | `v0.1.0-preview.1` |
| Spice Agent | `v0.0.0-20260806183953-eaf19180429a` |
| OpenAI Go SDK | `v3.50.0` |
| Provider API | Exact `model.Provider` / `model.Stream` implementation |
| Activation | Direct `New` or explicit `/autoconfigure` blank import fallback |
| Operating systems | Windows, Linux, and macOS |
| Network | HTTPS OpenAI-compatible Responses endpoint; no construction-time network activity |
| Tests | Offline scripted/fake-TLS tests by default; explicitly enabled live acceptance |

Request model selection is always `model.Request.Model`. The adapter supports
provider-neutral text, function calls/results, function definitions, usage,
completion, and bounded extension metadata. Unknown message extensions and
provider-hosted executable output are rejected.

Pre-1.0 releases may revise the adapter contract. Compatibility is ordinary Go
module selection and is recorded in `spice-compatibility.json` and the starter
manifest.
