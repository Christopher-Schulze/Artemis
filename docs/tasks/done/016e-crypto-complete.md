# TASK 016e: crypto.subtle complete

## Why

Closes the WebCrypto gap. All algorithms a modern auth/JWE/JWS library uses now work end-to-end via Go's `crypto/*` and `golang.org/x/crypto`.

## Done

### JWK import
- [x] `crypto.subtle.importKey('jwk', jwkObj, algo, ext, usages)` for `kty: oct` (HMAC, AES, HKDF, PBKDF2), `kty: RSA` (public + private with d/p/q), `kty: EC` (P-256/P-384/P-521 public + private)
- [x] base64url decoding via `encoding/base64.RawURLEncoding`
- [x] CryptoKey shape correct: `type` ('public'/'private'/'secret'), `algorithm`, `usages`

### Key derivation
- [x] `crypto.subtle.deriveBits({name:'HKDF', hash, salt, info}, key, bits)` via `golang.org/x/crypto/hkdf`
- [x] `crypto.subtle.deriveBits({name:'PBKDF2', hash, salt, iterations}, key, bits)` via `golang.org/x/crypto/pbkdf2`

### RSA full
- [x] generateKey for `RSA-PSS`, `RSA-OAEP`, `RSASSA-PKCS1-v1_5` (was only RSA-PSS)
- [x] `crypto.subtle.encrypt({name:'RSA-OAEP', label?}, pubKey, data)` via `rsa.EncryptOAEP`
- [x] `crypto.subtle.decrypt({name:'RSA-OAEP', label?}, privKey, ct)` via `rsa.DecryptOAEP`
- [x] `crypto.subtle.sign({name:'RSASSA-PKCS1-v1_5'}, privKey, data)` via `rsa.SignPKCS1v15`
- [x] `crypto.subtle.verify({name:'RSASSA-PKCS1-v1_5'}, pubKey, sig, data)` via `rsa.VerifyPKCS1v15`

### AES additional modes
- [x] `crypto.subtle.encrypt({name:'AES-CBC', iv}, key, data)` via `cipher.NewCBCEncrypter` + PKCS7 padding
- [x] `crypto.subtle.decrypt({name:'AES-CBC', iv}, key, ct)` via `cipher.NewCBCDecrypter` + unpadding

### Tests (7 new)
- [x] RSA-OAEP encrypt/decrypt roundtrip
- [x] AES-CBC encrypt/decrypt roundtrip
- [x] RSASSA-PKCS1-v1_5 sign/verify (256-byte sig for RSA-2048)
- [x] HKDF deriveBits returns correct length
- [x] PBKDF2 deriveBits returns correct length
- [x] JWK import RSA public → kty=RSA → CryptoKey{type=public, alg=RSA-PSS}
- [x] JWK import EC P-256 → kty=EC, crv=P-256 → CryptoKey{type=public, alg=ECDSA, namedCurve=P-256}

## Crypto.subtle final coverage

| Op | Algorithms |
|---|---|
| digest | SHA-1, SHA-256, SHA-384, SHA-512 |
| sign / verify | HMAC, RSA-PSS, RSASSA-PKCS1-v1_5, ECDSA (P-256/384/521) |
| encrypt / decrypt | AES-GCM, AES-CBC, RSA-OAEP |
| generateKey | HMAC, AES-* (128/192/256), RSA-PSS, RSA-OAEP, RSASSA-PKCS1-v1_5, ECDSA |
| importKey | 'raw' for HMAC + AES + HKDF + PBKDF2; 'jwk' for HMAC, AES, RSA, EC |
| exportKey | 'raw' for extractable secret keys |
| deriveBits | HKDF, PBKDF2 |

## Out of scope (would still extend)

- AES-CTR, AES-KW (key wrap), AES-CFB
- RSA-OAEP additional hash flexibility (currently bound at key creation)
- importKey 'pkcs8' / 'spki' (DER-encoded RSA/EC private/public keys; needs `crypto/x509.ParsePKCS8PrivateKey` plumbing)
- ECDH `deriveBits` for shared-secret derivation
- wrapKey / unwrapKey wrappers (basically encrypt+exportKey of another key's bytes)
- secp256k1 (Bitcoin) - not in Go stdlib

These all build on the existing infrastructure in 1-2 days each.
