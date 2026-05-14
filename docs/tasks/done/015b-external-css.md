# TASK 015b: External stylesheet loading

## Why

Closes the CSS support gap: `<link rel=stylesheet href=...>` now actually applies. Style cascade includes external sheets in document order along with `<style>` tags.

## Done

- [x] `js.StylesheetLoader` callback type in ContextOpts
- [x] `engine.Engine.stylesheetLoader(pageURL)` resolves href against page URL, fetches via engine HTTP client
- [x] `js.newStyleManager` walks `<link rel=stylesheet>` and adds parsed sheets after inline `<style>` tags
- [x] 404 / network error tolerated: that sheet is skipped, others still apply
- [x] tests: external CSS `.card { background: navy }` actually applies via getComputedStyle
- [x] tests: 404 stylesheet doesn't kill page, inline still works

## Notes

External sheets are loaded synchronously from the engine HTTP client during Context construction, in document order. With 50+ stylesheets this serializes; future TASK can switch to parallel via the goroutine path now that async-runtime exists.

`@import` inside the loaded stylesheet is NOT followed (the @-rule skip in stylesheet parser eats them). Real browsers follow imports recursively; deferring to a future TASK.
