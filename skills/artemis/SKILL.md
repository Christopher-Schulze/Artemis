# artemis — headless browser engine for agents

`artemis` is a single Go binary that fetches and renders web pages for agents.
Two kernels: a **renderless V8 path** (no Chrome needed, fast, low memory,
Chrome-identical on the wire) and a **Chromium/CDP path** (real browser when a
page needs it). This file is the complete operating contract — do not guess
flags; everything below is verified.

## Decide which command (do not overthink)

| You need | Command |
|---|---|
| Page text/content to read | `artemis fetch <url>` |
| Just the links or structured data | `artemis fetch --dump links|structured|semantic <url>` |
| Run JS on the page / extract a value | `artemis fetch --eval '<expr>' <url>` |
| Click, type, navigate in a real browser | `artemis act --request '<json>' <url>` |
| See what's on a real browser page | `artemis observe <url>` |
| Persistent session / many steps | `artemis serve` then WebSocket |
| What this build can do | `artemis capabilities` |
| Is the environment healthy | `artemis doctor` |

**Default is `fetch`.** Only reach for `act`/`observe` (real Chromium) when
fetch's output proves the page needs real rendering (missing content, empty
semantic tree, JS-required). Escalation is the cost model: fetch ≈ ms & MBs,
Chromium ≈ seconds & hundreds of MB.

## fetch — content extraction

```
artemis fetch [flags] <url>
```

stdout = the dump. stderr = diagnostics. Exit 0 ok, 1 runtime error, 2 bad args.

| Flag | Meaning |
|---|---|
| `--dump` | `markdown` (default, LLM-ready) · `text` · `html` · `title` · `links` (TSV: `href\ttext`) · `structured` (JSON: JSON-LD/microdata) · `semantic` (semantic tree, text) |
| `--eval <expr>` | evaluate a JS expression post-load; result replaces the dump on stdout |
| `--run-scripts` | execute inline `<script>` tags |
| `--header k=v` | extra request header; repeatable |
| `--user-agent <ua>` | override UA (default is a host-matched Chrome identity — leave it) |
| `--proxy <url>` | route through a proxy |
| `--timeout 30s` | request timeout |
| `--max-body-bytes N` | cap response size |
| `--allow-private-networks`, `--allow-port N` | opt out of SSRF guards |

Token-efficient usage: prefer `--dump markdown` for reading, `--dump links`
before navigating (JSON, small), `--eval` to pull a specific value instead of
dumping whole pages.

## observe + act — real Chromium

```
artemis observe [flags] <url>          # bounded DOM/AX snapshot as JSON
artemis act --request '<json>' <url>   # one typed action, evidence JSON out
```

`--request` schema: `{"kind": "<kind>", "ref"|"url"|"text"|"key"|"expression"|...}`.
Kinds: `navigate reload back forward wait focus click hover scroll key type
clear fill fill_form select check uncheck drag tab_open tab_close tab_list
tab_switch evaluate assert viewport frame_evaluate`.

`observe` emits stable element `ref`s; `act` consumes them (`ref` for
click/fill/hover, `targetRef` for drag). Example flow:

```sh
artemis observe https://site.test          # → {nodes:[{ref:"n12",...}], ...}
artemis act --request '{"kind":"click","ref":"n12"}' https://site.test
```

Every `act` result carries postcondition evidence — check `postcondition.passed`
before continuing. Budgets are real: `--max-cpu-percent 400`,
`--max-memory-bytes 2G`, `--timeout 30s`, `--session-timeout 30m`.

## serve — persistent sessions

`artemis serve` exposes JSON-over-WebSocket steering — use it for many
steps on one page or session reuse; one-shot work stays on the CLI.

Frames: `{"id": "<id>", "cmd": "<cmd>", "params": {...}}` →
`{"id": ..., "ok": bool, "result"|"error": ...}`. Commands:
`session.new`, `session.close`, `session.list`, `page.open`, `page.close`,
`page.eval`, `page.dump`, `page.click_by_text`, `page.type`,
`page.wait_idle`, `page.assert`, `chromium.act`, `stream`, `cancel`,
`token.rotate`, `heartbeat`, `capabilities`, `version`.

## Stealth (only when asked)

`ARTEMIS_STEALTH=stealth|paranoid` + `ARTEMIS_STEALTH_PURPOSE=<why>` +
`ARTEMIS_STEALTH_LEGAL_BASIS=<basis>` enables measured-profile
anti-detection. It is fail-closed: no acknowledgement → hard error, never a
silent downgrade. Default renderless fetch is already Chrome-coherent (UA,
Sec-CH-UA headers, JA3 via uTLS) — stealth is for hardened targets.

## Errors and exits

0 = success · 1 = runtime failure · 2 = bad invocation. Policy denials
(private IP, robots, download limits) are typed errors on stderr, not silent
emptiness. `artemis doctor` diagnoses environment problems (missing Chrome,
CGO, permissions).
