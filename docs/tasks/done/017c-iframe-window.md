# TASK 017c: iframe window plumbing

## Done

- [x] `window.parent` returns `window` (no real frame tree, top is self)
- [x] `window.top` returns `window`
- [x] `window.self` returns `window`
- [x] `window.frames` returns array of contentWindow objects for every iframe currently in the DOM (live; computed at access)
- [x] `window.opener` is `null`
- [x] `window.frameElement` is `null`
- [x] tests: identity checks + frames length matches iframe count

## Notes

This is "scripts that test for embedding behave correctly even though we don't isolate frames". A site that does `if (window.parent !== window)` to detect being embedded sees `false` here, which is the same as a real top-level page. `window.frames.length` gives the live iframe count for parent code that walks frames.

What's NOT here (TASK 017b, deferred): real per-frame V8 realm, cross-origin enforcement, MessagePort, full frame-tree semantics where iframe scripts see their own window vs parent.
