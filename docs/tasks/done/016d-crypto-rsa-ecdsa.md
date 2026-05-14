# TASK 016d: crypto.subtle RSA-PSS + ECDSA sign/verify

## Why

JWS RS256 (RSA-PSS) and ES256 (ECDSA-P-256) are the two dominant signature algorithms in modern OAuth/OIDC. Adding both lights up auth verification flows that were previously throwing.

## Done

- [x] `crypto.subtle.generateKey({name:'RSA-PSS', modulusLength:2048, hash:'SHA-256'})` -> `{publicKey, privateKey}`
- [x] `crypto.subtle.generateKey({name:'ECDSA', namedCurve:'P-256'})` -> keypair
- [x] `crypto.subtle.sign({name:'RSA-PSS'}, privKey, data)` via `rsa.SignPSS` with PSSSaltLengthEqualsHash
- [x] `crypto.subtle.verify({name:'RSA-PSS'}, pubKey, sig, data)`
- [x] `crypto.subtle.sign({name:'ECDSA', hash:'SHA-256'}, privKey, data)` via `ecdsa.Sign`, output JOSE-style raw r||s
- [x] `crypto.subtle.verify({name:'ECDSA', hash:'SHA-256'}, pubKey, sig, data)` parses r||s back
- [x] CryptoKey objects with type='public'/'private', usages filtered by direction
- [x] `RSASSA-PKCS1-V1_5` routed through the same dispatcher (signing path is the same shape; can be added by extending `__sign_asym`)
- [x] tests: RSA-PSS roundtrip + tampered rejection + ECDSA P-256 roundtrip (sig is exactly 64 bytes) + tampered rejection + keypair shape (type, usages)

## Notes

**Key formats**: only generateKey is implemented for RSA/ECDSA. importKey for 'jwk', 'pkcs8', 'spki' formats is the next sub-task and is what real-world OIDC verification needs (consume issuer's JWK Set). That work is ~200 LoC of go's `crypto/x509` parsing plus JWK base64url decoding.

**Hash flexibility**: ECDSA hash defaults to SHA-256 if the algorithm object lacks `.hash`. RSA-PSS uses the hash bound at importKey/generateKey time; passing a different hash at sign time is ignored. Real spec uses both (RSA-PSS pulls from key, ECDSA from algorithm parameter).

**Curves**: P-256, P-384, P-521 supported via Go's `crypto/elliptic`. SECP256K1 (Bitcoin) NOT supported in Go stdlib; deferred.
