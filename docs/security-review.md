# Security review

The trust boundary begins at application-owned configuration and request data
and ends at the configured HTTPS Responses endpoint. This trusted native module
does not claim to sandbox network activity.

| Risk | Control and evidence |
|---|---|
| Ambient credential capture | Only explicit `Config.APIKey` is accepted; the adapter does not use SDK environment defaults. |
| Credential or content disclosure | Secret tags, redacted `Config.String`, typed bounded errors, safe causes, metadata allowlist, and adversarial leak tests. |
| Endpoint credential exfiltration | Base URL must be absolute HTTPS with a host and no URL user information. |
| Hidden provider data retention | Every request sets `store=false`; the provider exposes no raw SDK accessor. |
| Duplicate side effects | A hashed operation idempotency key protects startup attempts; no provider retry occurs after a stream is returned. |
| Partial tool execution | Only finalized function calls in `response.completed` become Spice calls; malformed, duplicate, undeclared, and hosted calls fail closed. |
| Unbounded output | Core 4 MiB operation-text and 128 tool-call caps plus provider input/output-item bounds and 64 KiB translated chunks. |
| Unbounded wait or goroutine accumulation | Caller operation context, two-minute default and thirty-minute maximum configured timeout, one bounded stream pump, per-`Recv` context selection, and joined cooperative close. |
| Protocol/transport leakage | Startup and receive errors are typed separately; public text is generic, safe metadata is allowlisted, and raw upstream failures are not exposed. |
| Global cross-tenant state | One client/service/HTTP transport per injected provider bean; no registry or package-level mutable client. |
| Accidental live traffic | Unit/integration tests use scripted sources or fake TLS HTTP; live acceptance requires an explicit environment switch and credentials. |

Residual trust is explicit: application code supplies the HTTP client and
endpoint policy, and the OpenAI SDK performs the HTTPS exchange. A custom
transport can observe requests and credentials and must be treated as trusted
application code. A transport that ignores context cancellation and close can
prevent cooperative shutdown; the provider does not claim forcible containment.
Provider metadata is discarded by the engine unless the application explicitly
allowlists `openaiprovider.MetadataNamespace`.
