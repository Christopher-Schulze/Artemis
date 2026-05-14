# TASK 016b: crypto.subtle HMAC + key management

## Why

JWT, signed cookies, webhook verification - HMAC is the most common
crypto.subtle primitive. Without it, common auth flows throw
ReferenceError on `crypto.subtle.sign`.

## Done

- [x] `crypto.subtle.importKey('raw', keyBytes, {name:'HMAC', hash:'SHA-X'}, extractable, usages)`
- [x] `crypto.subtle.generateKey({name:'HMAC', hash:'SHA-X', length: bits}, ...)` - random key via `crypto/rand`
- [x] `crypto.subtle.sign({name:'HMAC'}, key, data)` returns Promise<bytes>
- [x] `crypto.subtle.verify({name:'HMAC'}, key, signature, data)` returns Promise<bool> via `hmac.Equal`
- [x] `crypto.subtle.exportKey('raw', key)` for extractable keys
- [x] CryptoKey JS object with `__id`, `type`, `extractable`, `algorithm`, `usages`
- [x] keys stored in process-global cryptoKeyStore (raw bytes never leave Go side except via exportKey)
- [x] tests: HMAC-SHA-256 sign+verify roundtrip, tampered-signature rejection, generateKey property shape, exportKey for extractable

## Notes

**Out of scope** - tracked for future TASKs:
- RSA-OAEP encrypt/decrypt
- AES-GCM/AES-CBC encrypt/decrypt
- RSA-PSS / ECDSA sign/verify
- ECDH / HKDF / PBKDF2 deriveBits
- importKey for jwk / spki / pkcs8 formats
- wrapKey / unwrapKey

These add another 600-800 LoC and need careful key-format handling. HMAC is the highest-leverage subset for agent auth flows.
