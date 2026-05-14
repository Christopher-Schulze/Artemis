# WebAPI long-tail batch (TASKs 023, 026, 028, 029, 031, 032, 033, 034)

## Why

Eight WebAPI surfaces in one batch. None individually is large, but together they cover ~30% of the long-tail surface that modern libraries feature-detect.

## Done

- [x] **023** Canvas 2D context stub: `getContext('2d')` returns a `CanvasRenderingContext2D`. All methods present (fillRect, beginPath, arc, drawImage, measureText, etc.). No actual rendering. `toDataURL`/`toBlob` return placeholder. `width`/`height` reflect the attribute.
- [x] **026** Web Animations API skeleton: `Animation`, `KeyframeEffect`, `element.animate()` returns settled Animation. `getAnimations()` returns []. No tweening, but feature detection passes.
- [x] **028** BroadcastChannel: in-process channel registry, postMessage delivers to other peers via microtask, addEventListener('message')/onmessage hooks.
- [x] **029** ReadableStream/WritableStream/TransformStream skeletons. ReadableStream with start(controller) callback, getReader().read() returns Promises with {value,done}. Pipe methods are no-ops.
- [x] **031** ShadowRoot stub: attachShadow returns a thin wrapper around the same element (light DOM). innerHTML/childNodes/querySelector delegated to host. Mode honored as data only.
- [x] **032** Form validity API: ValidityState computes valueMissing + patternMismatch from required/pattern attrs. element.checkValidity()/reportValidity()/setCustomValidity()/validity/validationMessage/willValidate.
- [x] **033** Range + Selection: full method surface (setStart, setEnd, selectNodeContents, cloneRange, ...). Selection with addRange/getRangeAt/removeAllRanges. document.createRange/getSelection.
- [x] **034** HTMLElement subclasses: HTMLElement + 40+ subclasses (HTMLInputElement, HTMLAnchorElement, HTMLImageElement, HTMLScriptElement, HTMLFormElement, HTMLTableElement, HTMLCanvasElement, HTMLAudioElement, HTMLVideoElement, etc.). Each is a constructor with `Symbol.hasInstance` matching by tagName, so `el instanceof HTMLInputElement` works correctly.
- [x] HTMLMediaElement stubs: play/pause/load/canPlayType, currentTime/duration/paused/readyState/volume/muted as properties. Audio/video pages no longer crash at boot.
- [x] tests for each surface

## Notes

These are stubs - they cover the surface required for feature detection and "polite degradation". A page that merely uses them won't crash. A page that depends on actual canvas pixels, real animation tweening, or shadow-DOM encapsulation still won't work end-to-end. Documented trade-off.

The instanceof support uses `Symbol.hasInstance` rather than real prototype chains because our element wrappers all share a single ELEM_PROTO; hooking real prototype chains would require per-tag wrap factories. Symbol.hasInstance gives correct semantics for `el instanceof HTMLInputElement` checks without that overhead.
