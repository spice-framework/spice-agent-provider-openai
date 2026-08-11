# Security review

The trust boundary begins at application-owned configuration and request data
and ends at the configured Responses endpoint. Remote endpoints require HTTPS;
plain HTTP is limited to a local loopback test bridge. This trusted native
module does not claim to sandbox network activity.

| Risk | Control and evidence |
|---|---|
| Ambient credential capture | Only explicit `Config.APIKey` is accepted; the adapter does not use SDK environment defaults. |
| Credential or content disclosure | Secret tags, redacted `Config.String`, typed bounded errors, safe causes, metadata allowlist, and adversarial leak tests. |
| Endpoint credential exfiltration | Base URL must be absolute HTTPS with a host and no URL user information. HTTP is accepted only for exact `localhost` or a parsed loopback IP literal; lookalike DNS names, wildcard addresses, and remote hosts fail closed. |
| Hidden provider data retention | Every request sets `store=false`; the provider exposes no raw SDK accessor. |
| Duplicate side effects | A hashed operation idempotency key protects startup attempts; no provider retry occurs after a stream is returned. |
| Partial tool execution | Only finalized function calls in `response.completed` become Spice calls; malformed, duplicate, undeclared, and hosted calls fail closed. |
| Unbounded output | Core 4 MiB operation-text and 128 tool-call caps plus provider input/output-item bounds and 64 KiB translated chunks. |
| Unbounded wait or goroutine accumulation | Caller operation context, two-minute default and thirty-minute maximum configured timeout, one bounded stream pump, per-`Recv` context selection, and joined cooperative close. |
| Protocol/transport leakage | Startup and receive errors are typed separately; public text is generic, safe metadata is allowlisted, and raw upstream failures are not exposed. |
| Global cross-tenant state | One client/service/HTTP transport per injected provider bean; no registry or package-level mutable client. |
| Accidental live traffic | Unit/integration tests use scripted sources or fake TLS HTTP. Live acceptance is excluded without the `openai_live` build tag and also requires one exact opt-in, credentials, explicit HTTPS base URL, and explicit model. The two opt-ins are mutually exclusive and neither has a default-network fallback. Ordinary gates remain offline. |
| Live acceptance secret leakage | The live test never prints configuration, response text, or upstream error details; configuration errors are value-free, and unit tests prove unconditional error redaction. Committed evidence excludes prompts, output, tokens, keys, and endpoint URLs. |
| Unbounded live acceptance cost or output | One model request, no tools, `store=false`, zero SDK retries, a 90-second timeout, a 32-token provider cap, 128 observed events, and 4 KiB accumulated text. OpenRouter mode additionally requires the exact `:free` suffix and zero prompt/completion catalog prices before inference. |

Residual trust is explicit: application code supplies the HTTP client and
endpoint policy, and the OpenAI SDK performs the HTTPS exchange. A custom
transport can observe requests and credentials and must be treated as trusted
application code. A transport that ignores context cancellation and close can
prevent cooperative shutdown; the provider does not claim forcible containment.
Provider metadata is discarded by the engine unless the application explicitly
allowlists `openaiprovider.MetadataNamespace`.

Loopback HTTP provides no transport confidentiality or peer authentication.
It is intended only for a trusted local OpenAI-compatible test bridge with
local-only credentials. The conventional `localhost` alias still depends on
the operating system's host resolution, so applications requiring a stricter
boundary should configure an IP loopback literal or HTTPS.
