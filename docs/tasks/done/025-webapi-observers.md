# TASK 025 (WebAPI batch 6): Microtask + Observer stubs + rAF/rIC + Event subtypes batch + FormData + live HTMLCollections

## Why

Single bundle of small WebAPI surfaces.

## Acceptance

- [x] `queueMicrotask(fn)` via `Promise.resolve().then(fn)`
- [x] `requestIdleCallback`/`cancelIdleCallback` (fires via setTimeout pump)
- [x] `requestAnimationFrame`/`cancelAnimationFrame` (fires via setTimeout pump)
- [x] `IntersectionObserver`/`ResizeObserver`/`ReportingObserver` stubs (never fire, we don't render)
- [x] PointerEvent, WheelEvent, TouchEvent, CompositionEvent, DragEvent, ProgressEvent, MessageEvent, ErrorEvent, StorageEvent, HashChangeEvent, PopStateEvent, BeforeUnloadEvent, CloseEvent, AnimationEvent, TransitionEvent
- [x] `FormData(form)` populates from form fields, supports get/getAll/has/delete/append/set/iterators
- [x] `document.forms`/`images`/`links`/`scripts` live (computed at access)
- [x] tests

## Notes

Observers are stubs because we don't render. Calls to observe()/disconnect() succeed but no records are produced. This is acceptable for SPA boot code that just feature-detects.
