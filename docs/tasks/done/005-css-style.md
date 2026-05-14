# TASK 005 (Phase 4): CSS minimal

## Why

A minority of agents care about layout, but a meaningful fraction read inline `element.style.color` or call `getComputedStyle(el).color`. We ship the inline-style path: parse the `style` attribute into key->value, expose `.style` reads and writes, and provide a `getComputedStyle(el)` that returns inline-only declarations. Cascade, inheritance, computed lengths, media queries, and a real CSS parser are explicitly out of scope here; they ship in a future TASK.

## Acceptance

- `css/inline.go`: `ParseInline(s string) map[string]string` and `Serialize(m map[string]string) string` (kebab-case keys).
- JS-side `element.style` returns a Proxy-like object: `el.style.color`, `el.style.fontSize` (camelCase getter / setter), backed by the element's `style` attribute.
- JS-side `getComputedStyle(el)` returns the same shape; today it equals inline only.
- Tests for parser + JS surface.
- All green.

## Sub-Tasks

- [x] `css/inline.go` + tests
- [x] JS bootstrap: Element.prototype `style` getter, `getComputedStyle` global
- [x] tests
- [x] FLUSH
- [x] archive

## Notes

Style attribute serialisation rebuilds the string from the map every set so order is not preserved; that matches Lightpanda's documented behaviour and is fine for agent reads.

camelCase <-> kebab-case mapping uses the standard rule (insert hyphen before each uppercase letter, lowercase it). `webkit`/`moz` vendor prefixes are not treated specially.

## Deviations

(none)
