# TASK 043: HTTP/2 pool tuning

## Why

Crawler-style workloads make many parallel subresource fetches against a small set of hosts. Default `net/http` pool (idle-conns 2/host) becomes the bottleneck. Tuned values: 64 idle conns/host, 5s TLS timeout, explicit response-header timeout, 64KB read+write buffers.

## Done

- [x] aggressive pool config in `network.NewHTTPClient`
- [x] HTTP/2 forced on (`ForceAttemptHTTP2: true`) - was already on but kept explicit
- [x] response-header timeout to fail fast on stuck origins
- [x] all tests still green

## Numbers

Localhost benchmark numbers don't move materially (httptest is in-process; no net/http pool path is exercised). Real-world impact: estimated 30-50% throughput improvement for crawl workloads with 10+ parallel fetches per host. Not measured this session because it requires a real-world fixture set, which is TASK 047b.
