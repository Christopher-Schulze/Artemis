# TASK 015 (Phase A6): CSS parser + cascade (minimum-viable)

## Why

Last meaningful gap to Lightpanda WebAPI. CSS-in-JS frameworks read computed styles to make layout decisions; without a real cascade `getComputedStyle(el).backgroundColor` returns nothing for a class declared in `<style>`. Minimum-viable here: parse `<style>` text, selector matching via cascadia, specificity-aware cascade, merged with inline style. External `<link rel=stylesheet>` and full computed-length resolution arrive in a later TASK.

## Acceptance

- `css/stylesheet.go`: `ParseStylesheet(src) *Stylesheet` with rules of (selector, declarations).
- `css/cascade.go`: `Cascade(doc, node) map[string]string` returns the resolved declarations for a node, ordered by specificity (cascadia provides), with inline style applied last and `!important` honored.
- StyleManager collects every `<style>` text in the document at Context init.
- JS-side `getComputedStyle(el).<prop>` reads from the cascade; setting properties still updates inline style only (cascade is read-through).
- All green, race-clean.

## Sub-Tasks

- [x] CSS rule tokenizer (find balanced rule blocks)
- [x] declaration block parser (re-using `css.ParseInline`)
- [x] cascade resolver with cascadia selector matching + specificity sort
- [x] StyleManager collects sheets
- [x] JS bridge: getComputedStyle queries cascade
- [x] tests

## Notes

What's intentionally NOT in this minimum-viable:
- External `<link rel=stylesheet>` loading - same plumbing as external scripts (TASK 010); add as TASK 015b.
- @media, @supports, @keyframes - skipped (rules inside @media are ignored, not applied always).
- Computed lengths (em/rem/% to px), shorthand expansion (border -> border-color/style/width), color normalization (`red` -> `rgb(255,0,0)`) - the cascade returns property values verbatim from the stylesheet.
- inherited/initial cascade values - we don't walk the parent chain to inherit; the agent layer rarely cares.

These limitations are explicit; everything else needed for "does this class apply to this element" works correctly.

## Deviations

(none)
