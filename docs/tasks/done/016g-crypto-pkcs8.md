# TASK 016g: crypto.subtle importKey 'pkcs8' / 'spki'

## Done

- [x] `crypto.subtle.importKey('pkcs8', der, algo, ext, usages)` for RSA/EC private keys via Go's `crypto/x509.ParsePKCS8PrivateKey`
- [x] `crypto.subtle.importKey('spki', der, algo, ext, usages)` for RSA/EC public keys via `x509.ParsePKIXPublicKey`
- [x] CryptoKey shape correct: `type='private'/'public'`, `algorithm` (with modulusLength for RSA, namedCurve for EC), `usages`, `extractable`
- [x] curve detection via Go's `Curve.Params().Name` ("P-256" / "P-384" / "P-521")

## Notes

PKCS8/SPKI are the binary DER-encoded forms used by OpenSSL, OIDC providers, and most JOSE-based auth libraries that don't speak JWK. PEM (-----BEGIN-----) wrapping is NOT done here - callers strip the PEM headers and base64-decode before passing the raw DER bytes (which is the CryptoKey API contract anyway).

Together with TASK 016e's JWK importKey, all three formats specified by WebCrypto for asymmetric keys are now supported: 'jwk', 'pkcs8', 'spki'. ('raw' covers symmetric keys.)
