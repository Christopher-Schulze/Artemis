# Artemis Smoke - First Honest Run

Date: 2026-07-04T19:24:41.757838+02:00
Binary: /tmp/artemis-smoke-bin
Scenarios: 10 total, 9 passed, 0 failed, 1 skipped

| ID | Site | Status | Notes |
|---|---|---|---|
| nav-example | https://example.com | PASS | 1 evidence artifacts |
| nav-wikipedia | https://en.wikipedia.org/wiki/Headless_browser | PASS |  |
| scrape-hn-headlines | https://news.ycombinator.com | PASS |  |
| scrape-httpbin-status | https://httpbin.org | PASS |  |
| form-httpbin-get | https://httpbin.org/forms/post | PASS | 1 evidence artifacts |
| nav-github-repo | https://github.com/golang/go | PASS | 1 evidence artifacts |
| nav-mdn-fetch | https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API | PASS | 1 evidence artifacts |
| challenge-cloudflare | https://nowsecure.nl | PASS | 1 evidence artifacts |
| login-httpbin-basic | https://httpbin.org/basic-auth/user/passwd | SKIP | missing env var ARTEMIS_SMOKE_BASIC_USER |
| nav-rust-lang | https://www.rust-lang.org | PASS | 1 evidence artifacts |

## Per-Scenario Step Summary

### nav-example - PASS
Site: https://example.com
Steps: 7 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open example.com (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 assert body text (page.assert)
  [OK  ] 04 dump markdown (page.dump)
  [OK  ] 05 close page (page.close)
  [OK  ] 06 close session (session.close)

### nav-wikipedia - PASS
Site: https://en.wikipedia.org/wiki/Headless_browser
Steps: 7 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open wikipedia (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 assert body text (page.assert)
  [OK  ] 04 dump links (page.dump)
  [OK  ] 05 close page (page.close)
  [OK  ] 06 close session (session.close)

### scrape-hn-headlines - PASS
Site: https://news.ycombinator.com
Steps: 6 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open hn (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 dump links (page.dump)
  [OK  ] 04 close page (page.close)
  [OK  ] 05 close session (session.close)

### scrape-httpbin-status - PASS
Site: https://httpbin.org
Steps: 5 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open httpbin status 200 (page.open)
  [OK  ] 02 assert status 200 (page.assert)
  [OK  ] 03 close page (page.close)
  [OK  ] 04 close session (session.close)

### form-httpbin-get - PASS
Site: https://httpbin.org/forms/post
Steps: 8 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open httpbin form (page.open)
  [OK  ] 02 type custname (page.type)
  [OK  ] 03 assert custname value (page.assert)
  [OK  ] 04 type custtel (page.type)
  [OK  ] 05 dump html (page.dump)
  [OK  ] 06 close page (page.close)
  [OK  ] 07 close session (session.close)

### nav-github-repo - PASS
Site: https://github.com/golang/go
Steps: 6 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open github go repo (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 dump markdown (page.dump)
  [OK  ] 04 close page (page.close)
  [OK  ] 05 close session (session.close)

### nav-mdn-fetch - PASS
Site: https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API
Steps: 6 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open mdn fetch (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 dump markdown (page.dump)
  [OK  ] 04 close page (page.close)
  [OK  ] 05 close session (session.close)

### challenge-cloudflare - PASS
Site: https://nowsecure.nl
Steps: 5 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open challenge site (page.open)
  [OK  ] 02 dump html (page.dump)
  [OK  ] 03 close page (page.close)
  [OK  ] 04 close session (session.close)

### login-httpbin-basic - SKIP
Site: https://httpbin.org/basic-auth/user/passwd
Skip reason: missing env var ARTEMIS_SMOKE_BASIC_USER

### nav-rust-lang - PASS
Site: https://www.rust-lang.org
Steps: 6 | Duration: 0ms
  [OK  ] 00 open session (session.new)
  [OK  ] 01 open rust-lang (page.open)
  [OK  ] 02 assert title (page.assert)
  [OK  ] 03 dump markdown (page.dump)
  [OK  ] 04 close page (page.close)
  [OK  ] 05 close session (session.close)
