# TASK 034b: HTMLInputElement / HTMLSelectElement / HTMLAnchorElement live properties

## Done

Boolean attribute properties (presence on attribute = true; assignment toggles attribute):
- [x] `.checked`, `.disabled`, `.readOnly`, `.required`, `.multiple`, `.autofocus`, `.selected`

String attribute properties:
- [x] `.name`, `.value` (live, not snapshot), `.placeholder`
- [x] textarea `.value` reads/writes textContent (per spec)

Numeric:
- [x] `.maxLength`, `.minLength`

Form association:
- [x] `.form` walks up parentNode chain to nearest `<form>`

Select-specific:
- [x] `.selectedIndex` (read/write)
- [x] `.options` (querySelectorAll('option'))
- [x] `.value` for `<select>` returns the selected option's value (or first option's value if none selected); writing finds option with matching value and selects it

Anchor URL parts (parsed via `__url_parse(href, location.href)`):
- [x] `.protocol`, `.hostname`, `.host`, `.port`, `.pathname`, `.search`, `.hash`, `.origin`

## Notes

Element wrapper objects (`__wrap(id)`) are fresh per call - `===` over wrappers compares object identity, not handle. Tests compare `.element.form.__id === otherElement.__id`. Real browsers cache wrappers per node; we could too via a per-Context wrapper cache but the current model is simpler and only an issue for `===` checks (rare in practice; framework code uses `.contains()` or `.matches()`).

Generic `.value` and `<select>.value` are layered: the select case wins by checking `tagName === 'SELECT'`. For anchor URL parts the parent's `.host` getter would normally be a no-op for non-anchor; we gate inside the getter to return `''` for non-A/AREA elements.
