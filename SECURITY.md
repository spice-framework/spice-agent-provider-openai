# Security policy

Report vulnerabilities privately through GitHub Security Advisories for this
repository. Do not include production credentials or sensitive response content
in an issue.

The provider never reads ambient OpenAI environment variables. Applications
must pass credentials through typed, secret-marked Spice configuration. Errors,
events, logs, manifests, generated files, and test fixtures must not contain API
keys, authorization headers, prompt bodies, or model output unless the owning
application explicitly chooses to retain content.

Remote API endpoints require HTTPS. Plain HTTP is accepted only for exact
`localhost` or a parsed loopback IP literal and is intended solely for trusted
local test bridges; it provides no transport confidentiality or peer
authentication. Construction performs no network I/O. Every request must use a
caller-owned context and bounded timeout. Automatic retry is limited to
failures known to happen before streaming starts; an ambiguous or partial
stream is never replayed automatically.

Supported versions receive security fixes on the latest preview line until a
stable support policy is published.

The optional live acceptance is a real network operation. It is excluded from
ordinary tests and requires both the `openai_live` build tag and one exact,
mutually exclusive opt-in. Canonical `SPICE_RESPONSES_LIVE=1` mode requires an
explicit compatible HTTPS base URL and model; OpenRouter additionally requires
an exact `:free` route whose public catalog reports zero prompt and completion
prices. Optional `SPICE_OPENAI_LIVE=1` mode accepts only the exact explicit
first-party OpenAI base URL and may be billable. The failure path discards
upstream details rather than risk printing credentials or response content.
Never place a key in a command-line argument, source file, fixture, issue, or
captured test artifact.
