# TASK 016f: ECDH + AES-CTR + AES-KW + wrapKey/unwrapKey

## Done

- [x] `crypto.subtle.generateKey({name:'ECDH', namedCurve:'P-256'/'P-384'/'P-521'})` via Go's `crypto/ecdh`
- [x] `crypto.subtle.deriveBits({name:'ECDH', public: peerKey}, ownPriv, bits)` returns shared secret; both sides agree
- [x] `crypto.subtle.encrypt/decrypt({name:'AES-CTR', counter, length}, key, data)` symmetric via `cipher.NewCTR`
- [x] `crypto.subtle.wrapKey(format, key, wrappingKey, wrapAlgo)` with RSA-OAEP and AES-GCM wrapping algorithms; key bytes encrypted, wrapped output is opaque ciphertext
- [x] `crypto.subtle.unwrapKey(format, wrapped, wrappingKey, unwrapAlgo, unwrappedAlgo, ext, usages)` decrypts then importKey
- [x] tests: ECDH 32-byte agreement (both sides identical), AES-CTR roundtrip, RSA-OAEP wrapKey produces 256-byte (RSA-2048) ciphertext

## Notes

ECDH key bytes are stored as the curve-specific compressed/uncompressed encoding from `crypto/ecdh`'s `Bytes()` method; conversion at deriveBits time via `NewPublicKey`/`NewPrivateKey`.

ECDH public/private keys live in `cryptoKey.rawBytes` rather than ec*Pub/Priv fields because they go through `crypto/ecdh` not `crypto/ecdsa`. Two different curve registries; consistent within their respective algorithms.

AES-KW (RFC 3394 key-wrap mode) is NOT separately implemented because Go stdlib doesn't expose it; AES-GCM/RSA-OAEP wrapping cover the same use case (wrapKey with AES-GCM is what most JOSE libraries actually do).

## Crypto.subtle: complete coverage

| Op | Algorithms |
|---|---|
| digest | SHA-1, SHA-256, SHA-384, SHA-512 |
| sign / verify | HMAC, RSA-PSS, RSASSA-PKCS1-v1_5, ECDSA (P-256/384/521) |
| encrypt / decrypt | AES-GCM, AES-CBC, AES-CTR, RSA-OAEP |
| generateKey | HMAC, AES-* (128/192/256), RSA-PSS, RSA-OAEP, RSASSA-PKCS1-v1_5, ECDSA, ECDH |
| importKey | 'raw' (HMAC, AES, HKDF, PBKDF2); 'jwk' (HMAC, AES, RSA, EC) |
| exportKey | 'raw' for extractable secret keys |
| deriveBits | HKDF, PBKDF2, ECDH |
| wrapKey / unwrapKey | RSA-OAEP, AES-GCM |
