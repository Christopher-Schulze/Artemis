# TASK 011 (Phase A2): MutationObserver

## Why

Single biggest gap to modern framework compatibility. React/Vue/Angular/Solid use MutationObserver for reactivity, hydration, and shadow-DOM bookkeeping. Without it any non-trivial SPA breaks silently.

## Acceptance

- `MutationObserver` JS class: `observe(target, options)`, `disconnect()`, `takeRecords()`.
- options supported: `childList`, `attributes`, `subtree`. (`characterData`, `attributeOldValue`, `characterDataOldValue`, `attributeFilter` deferred.)
- `MutationRecord` shape: `type`, `target`, `addedNodes`, `removedNodes`, `attributeName`, `previousSibling`, `nextSibling`.
- Records batched per observer; callback fires once per script boundary with the accumulated batch (microtask semantics).
- Mutation hooks in the JS bridge: setAttribute, removeAttribute, appendChild, removeChild, insertBefore, set innerHTML, set textContent each push records.
- Subtree option: ancestor observers also notified when descendants mutate.
- All green.

## Sub-Tasks

- [x] add per-Context observer registry + pending records buffer
- [x] hook each mutation in `nodeCall` / `nodeSetProp` to record events
- [x] flush records to JS callbacks at the end of Eval (alongside fireTimers)
- [x] JS-side MutationObserver bootstrap that bridges to native helpers
- [x] tests: childList add/remove fires; attributes change fires; subtree=true catches grandchildren; disconnect stops; takeRecords drains pending

## Notes

We do NOT diff DOM state to derive records; we capture them at mutation time, which is faster and matches whatwg semantics exactly for the operations we support.

`characterData` mutations (TextNode.data assignments) require a separate Go-side hook on text-node mutations - deferred since current bridge doesn't expose text-node mutation as a JS-visible operation.

## Deviations

(none)
