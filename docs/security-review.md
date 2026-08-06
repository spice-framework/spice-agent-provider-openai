# Security review

The trust boundary begins at application-owned configuration and ends at the
configured HTTPS OpenAI endpoint. This module is trusted native code and does
not claim to sandbox network activity.

| Risk | Control |
|---|---|
| Ambient credential capture | The Responses service is built from explicit options and never uses SDK environment defaults. |
| Credential disclosure | Secret-marked configuration, redacted diagnostics, and payload-free general observations. |
| Endpoint credential exfiltration | Absolute HTTPS URL validation and rejection of URL user information. |
| Duplicate side effects | No automatic replay after any stream event or ambiguous response. |
| Unbounded wait | Caller context plus a positive bounded request timeout. |
| Retry amplification | Explicit retry count limited to 0–8 and restricted further by Phase 3 stream state. |
| Global cross-tenant state | One client and HTTP transport per injected provider bean. |

Phase 0 establishes construction and configuration controls. Streaming and
translation cannot be security-approved until the tagged core contracts and
Phase 3 failure tests exist.
