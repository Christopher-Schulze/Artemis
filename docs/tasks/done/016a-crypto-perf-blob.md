# TASK 016a (Phase A7 partial): Crypto.subtle.digest + Performance + Blob/File

## Why

Three small but very common WebAPI surfaces. Crypto.subtle.digest is what
hash-libraries call. Performance.now() shows up in nearly every JS performance
measurement. Blob/File underpin downloads, dropzones, and fetch body handling.

## Acceptance

- [x] `crypto.subtle.digest('SHA-256', uint8array)` returns a Promise<bytes>; SHA-1, SHA-384, SHA-512 also supported via Go's `crypto/sha*`
- [x] `performance.now()` returns ms since context creation; `timeOrigin` is the unix ms; mark/measure/clearMarks/getEntries are no-op stubs
- [x] `Blob` constructor (parts + options); `.size`, `.type`, `.text()`, `.arrayBuffer()`, `.slice()`
- [x] `File extends Blob` with `name`, `lastModified`
- [x] `FileReader` with `readAsText` / `readAsArrayBuffer` + `onload`
- [x] tests
- [x] FLUSH
- [x] archive

## Notes

`crypto.subtle.sign / verify / encrypt / decrypt / deriveBits / generateKey / importKey / exportKey / wrapKey / unwrapKey` are NOT implemented yet. Sign/verify alone is another full TASK because key formats span PKCS8, JWK, raw bytes; encrypt/decrypt need cipher selection and IV handling. Tracked in TASK 016 (Phase A7 full).

PerformanceObserver is not real here; mark/measure are no-ops. PerformanceObserver entries arrive in TASK 025.

Blob streaming (`Blob.stream()`) not implemented. Streams in general are TASK 029.
