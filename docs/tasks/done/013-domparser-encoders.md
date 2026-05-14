# TASK 013 (Phase A4): DOMParser + TextEncoder/TextDecoder + URL + URLSearchParams + Custom Elements

## Why

Five WebAPI surfaces that come up in nearly every modern script. Most are small in isolation but their absence breaks libraries silently.

## Acceptance

- `DOMParser` JS class; `parseFromString(src, mimeType)` returns a Document-shaped JS object with the same proxy API our main `document` uses (read-only).
- `TextEncoder` / `TextDecoder` for utf-8 (the only required encoding).
- `URL` JS class; constructor `new URL(href, base?)`; properties `href`, `protocol`, `host`, `hostname`, `port`, `pathname`, `search`, `hash`, `origin`, `searchParams`. Backed by `__url_parse`.
- `URLSearchParams`: `get`, `getAll`, `set`, `append`, `delete`, `has`, `toString`, `entries`, iterator.
- `customElements.define(name, ctor, opts)` + `customElements.get(name)` + `customElements.whenDefined(name)`. Light-DOM only; element upgrade is best-effort (calls `connectedCallback` on existing matching elements).

## Done sub-tasks

- [x] all of the above
- [x] tests
