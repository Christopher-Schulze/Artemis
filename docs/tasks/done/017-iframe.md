# TASK 017: iframe support (minimum-viable)

## Done

- [x] `iframe.contentDocument` returns a Document-shaped wrapper around the parsed sub-document
- [x] iframe HTML fetched via engine HTTP client on first contentDocument access (lazy)
- [x] querySelector / getElementById / body / documentElement / title work on contentDocument
- [x] `iframe.contentWindow` exposes location.href, document, postMessage, addEventListener('message')
- [x] postMessage between parent and contentWindow delivers via in-process MessageEvent dispatch
- [x] tests: parent reads iframe contentDocument h1 text; parent posts message to iframe and listener fires

## Important constraints (intentional)

**Same V8 realm**: iframe scripts share the parent's V8 context. This lets parent JS read iframe.contentDocument freely (matches same-origin behavior in real browsers). Cross-origin restriction enforcement is NOT implemented; agents are trusted by their embedder.

**Iframe scripts are NOT executed**: when the iframe HTML is parsed, its `<script>` tags are ignored. Real browsers spin up a separate JS realm per frame and run those scripts; we don't. For agent extraction (read iframe content) this is fine; for sites that depend on iframe-internal scripts, this is a known gap.

**No frame tree**: there's no top-level `frames` collection on window, no `parent`/`top` traversal across the boundary. window.parent === window (real browsers reflect this for same-origin same-frame). Future TASK 017b can add the frame tree.

These constraints make the implementation tractable in one session while delivering the 80% case for agent-driven iframe data extraction.
