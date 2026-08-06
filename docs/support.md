# Support and compatibility

| Contract | Phase 0 support |
|---|---|
| Go | Exactly 1.26.5 for development and verification |
| Spice core | `v0.1.0-preview.1` |
| Spice toolchain | `v0.1.0-preview.1` |
| OpenAI Go SDK | `v3.50.0` |
| Provider API | Concrete no-network client; provider-neutral adapter deferred until the tagged core API |
| Activation | Explicit `/autoconfigure` blank import or direct constructor |
| Operating systems | Windows, Linux, and macOS |
| Network | HTTPS OpenAI-compatible Responses endpoint; no construction-time network activity |

Pre-1.0 releases may revise the adapter contract. Compatibility is ordinary Go
module selection and is recorded in `spice-compatibility.json`.
