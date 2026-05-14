# TASK 022a (WebAPI batch 3 partial): Event subtypes

## Why

Listeners that do `instanceof MouseEvent` or read `event.clientX` need the
event class to exist. With these missing, framework code fails silently.

## Acceptance

- [x] `UIEvent`, `MouseEvent`, `KeyboardEvent`, `InputEvent`, `FocusEvent`, `CustomEvent` exposed as subclasses of `Event`
- [x] All standard properties present (`clientX/Y`, `key/code`, `data`, `relatedTarget`, `detail`, etc.)
- [x] tests cover construction + property reads

## Notes

PointerEvent, WheelEvent, TouchEvent, CompositionEvent, DragEvent,
AnimationEvent, TransitionEvent, ProgressEvent, MessageEvent, ErrorEvent
not yet implemented. Tracked in TASK 022 (full batch) - they are
straightforward subclass definitions that just take time to enumerate.

Native event sources (mouse/keyboard from a real driver) are out of scope -
agents construct events programmatically and dispatch via `el.dispatchEvent`.
