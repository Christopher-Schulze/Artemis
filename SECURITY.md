# Security Policy

## Supported Versions

Artemis is pre-1.0 software. Security fixes are applied to the latest `main`
branch and the most recent tagged release only.

| Version | Supported |
|---|---|
| latest main | yes |
| latest tag | yes |
| older tags | no |

## Reporting a Vulnerability

Report security vulnerabilities privately. Do NOT open a public GitHub issue
for a vulnerability.

- Email: security@christopher-schulze.dev
- Encrypt with the project PGP key if available.

Include:
- A clear description of the issue and impact.
- Reproduction steps (minimal input, command, or script).
- Affected version or commit.
- Any suggested fix or mitigation.

You will receive an acknowledgement within 72 hours. Coordinated disclosure
timelines are negotiated per report; we default to a 90-day disclosure window
unless the reporter requests otherwise.

## Threat Model (Summary)

Artemis is an embedded browser runtime that executes untrusted web content
inside a V8 isolate (renderless mode) or a Chromium process (CDP mode).

- **Renderless mode**: untrusted JS runs inside a V8 isolate with no network
  egress beyond the configured `fetch`/`XHR` bridge and the SSRF/private-IP
  guard. The isolate has no DOM layout/paint and no real browser APIs beyond
  the explicitly installed WebAPI shims.
- **Chromium mode**: untrusted pages run inside a sandboxed Chromium process
  with the stealth/network policy stack (`security/netguard.go`,
  `security/policy.go`). CDP commands are issued by the host, not the page.
- **Serve mode**: the standalone WebSocket server binds loopback by default
  and authenticates each session. Remote bindings require an explicit
  operator flag and an allowlist.

Out of scope for this document: host OS exploitation via V8/Chromium
memory-corruption bugs (upstream V8/Chromium security handles these);
operator-side credential handling (covered by the operator credential vault).

## Hardening Commitments

- The renderless V8 isolate never gains filesystem, process, or raw socket
  access. New WebAPI shims must be reviewed for SSRF and exfiltration surface.
- The Chromium launch flags disable the remote debugging port by default and
  scope CDP to the host-controlled pipe.
- The `serve` command refuses to bind `0.0.0.0` without an explicit
  `--bind-external` flag and an origin allowlist.
- Network egress policy (`security/policy.go`) is deny-by-default for private
  IP ranges and operator-configurable allowlists.

## Vulnerability Response Procedure

1. Triage the report within 72 hours.
2. Open a private advisory (GitHub Security Advisory when the public repo
   exists).
3. Develop a fix on a private branch.
4. Coordinate disclosure timing with the reporter.
5. Publish a patched release and a public advisory with credit (unless the
   reporter requests anonymity).
