# TASK 007 (Phase 6): Network stack

## Why

Production agents need to play nice: respect robots.txt, expose cookies to JS via `document.cookie`, optionally block requests to private/internal IP ranges, and let the embedder intercept outbound requests for caching, mocking, or quota control.

## Acceptance

- `network/robots.go` parses robots.txt and answers `Allowed(userAgent, urlPath)`. Cached per host inside `network.HTTPClient`.
- `engine.Config.ObeyRobots` causes `Fetch` to consult robots.txt for the host before the GET; disallowed URLs return `engine.ErrRobotsDisallowed`.
- `network/ipfilter.go` resolves the host, refuses connections to RFC1918, loopback, link-local, multicast when `engine.Config.BlockPrivateIPs` is set.
- `document.cookie` exposed in JS: getter returns serialized cookies for the current URL; setter parses one cookie attribute set and stores it via the engine's cookie jar.
- `engine.FetchOpts.OnRequest func(*RequestInfo) (*ResponseInfo, error)` interception hook: returning a non-nil ResponseInfo short-circuits the network call (mock/cache); returning nil + nil error proceeds normally.
- All green: build, vet, test, race.

- [x] `network/robots.go` (parser + per-host cache + Allowed) + tests (allow/disallow longest-prefix, empty=allow-all, nil-safe)
- [x] `network/ipfilter.go` (loopback/RFC1918/link-local/multicast/CGNAT) + tests (numeric-IP path)
- [x] `engine.Config.ObeyRobots` consults robots.txt before GET; `engine.ErrRobotsDisallowed` exported
- [x] `engine.Config.BlockPrivateIPs` rejects loopback / private host
- [x] `engine.FetchOpts.OnRequest` interception with `RequestInfo`/`ResponseInfo`; mock body parsed and exposed as a Page
- [x] `document.cookie` JS get/set via `__doc_get('cookie')` + `__doc_set_cookie`; `js.ContextOpts.GetCookie/SetCookie` callbacks; engine wires them to the cookie jar bound to the page URL
- [x] integration tests: robots-disallowed returns ErrRobotsDisallowed; loopback blocked; OnRequest mocks; `document.cookie` reads server-set cookies and write round-trips into the jar
- [x] all green: build, vet, test, race
- [x] FLUSH documentation.md
- [x] archive

## Notes

robots.txt parser supports User-agent / Disallow / Allow / Sitemap. Most-specific match wins. No support for crawl-delay (rare), wildcards in user-agent (uncommon).

`document.cookie` setter parses standard `Name=Value; Path=/; Max-Age=N` attributes. We store via `net/http/cookiejar.Jar.SetCookies` so subsequent requests carry the cookie.

Network interception is invoked from `network.HTTPClient.Do`. The hook signature uses an exported RequestInfo/ResponseInfo to keep the JS package free of `net/http` types.

Reference: internal design notes.

## Deviations

(none yet)
