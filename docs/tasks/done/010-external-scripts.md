# TASK 010 (Phase A1): External script loading

## Why

Without external `<script src=...>` execution, no modern SPA can boot. React/Vue/Angular/Solid/Svelte/etc. all ship as external scripts. This is the single highest-impact capability gap.

## Acceptance

- `engine.runScripts` (rename of `runInlineScripts`) walks the DOM, executes inline AND external scripts in document order.
- External scripts (`<script src=...>`) are fetched via `engine.HTTPClient.Do`, with URL resolution against the page URL, and executed against the same JS context.
- A per-page script cache prevents double-fetching the same URL.
- 404 / network error on a script logs a warning but does not abort the page.
- `defer` and `async` scripts both treated as blocking after the parse pass (sufficient for agents); accurate spec-ordering deferred.
- `engine.FetchOpts.RunInlineScripts` renamed to `engine.FetchOpts.RunScripts` (kept as alias).
- All green.

## Sub-Tasks

- [x] generalise the script walker
- [x] HTTP fetch + resolve url + run via jsCtx.Eval
- [x] cache by URL (per Page)
- [x] integration test: page with external script that builds DOM
- [x] FLUSH
- [x] archive

## Notes

Module scripts (`<script type="module">`) are run as classic for now (no real ES module loader). Spec-correct module loading is a separate TASK because it pulls in module specifier resolution + import maps.

## Deviations

(none)
