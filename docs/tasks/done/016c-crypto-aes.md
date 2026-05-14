# TASK 016c: crypto.subtle AES-GCM

## Done

- [x] `crypto.subtle.encrypt({name:'AES-GCM', iv, additionalData?}, key, plaintext)` via `crypto/aes` + `cipher.NewGCM`
- [x] `crypto.subtle.decrypt({name:'AES-GCM', iv, additionalData?}, key, ciphertext)` -> Promise<bytes>
- [x] `crypto.subtle.generateKey({name:'AES-GCM', length: 128/192/256}, ext, usages)` -> CryptoKey
- [x] `crypto.subtle.importKey('raw', bytes, {name:'AES-GCM'}, ext, usages)` -> CryptoKey
- [x] dispatch wrapper: `crypto.subtle.generateKey/importKey` now route by algorithm name (AES-* -> AES path; HMAC -> HMAC path) without breaking existing HMAC tests
- [x] tests: AES-GCM roundtrip with random key + random IV; importKey with raw 32-byte produces 256-bit AES-GCM key

## Notes

**Out of scope (next TASKs)**:
- AES-CBC, AES-CTR, AES-KW
- RSA-OAEP encrypt/decrypt
- RSA-PSS / ECDSA sign/verify
- ECDH / HKDF / PBKDF2 deriveBits
- importKey for jwk / spki / pkcs8 formats
- wrapKey / unwrapKey

The current AES path uses Go's `crypto/aes` which is constant-time on x86 with AES-NI; on Apple Silicon the same constant-time guarantees hold via runtime's optimized assembly.
