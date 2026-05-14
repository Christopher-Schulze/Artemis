# TASK 020 (WebAPI batch 1): Tree utilities

## Why

Tree iteration utilities + DOMRect + DocumentFragment - small classes that many libraries use for DOM walking and viewport math.

## Acceptance

- [x] `NodeFilter` constants (FILTER_ACCEPT/REJECT/SKIP, SHOW_ELEMENT/TEXT/...)
- [x] `TreeWalker` with `firstChild`, `nextSibling`, `nextNode`, `parentNode`
- [x] `NodeIterator` with `nextNode`
- [x] `document.createTreeWalker(root, whatToShow, filter)`
- [x] `document.createNodeIterator(...)`
- [x] `document.createDocumentFragment()` returns synthetic fragment
- [x] `DOMRect` constructor + properties
- [x] `Element.getBoundingClientRect()` returns zero-rect (we don't render)
- [x] `Element.matches`, `Element.closest`, `Element.contains`
- [x] `Element.dataset` Proxy + `Element.classList` (add/remove/toggle/contains)
- [x] tests

## Notes

DocumentType + DOMImplementation deferred - rarely used in agent code.
