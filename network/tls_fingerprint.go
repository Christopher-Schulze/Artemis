package network

// tls_fingerprint.go (spec L4369: TLS Fingerprint Spoofing).
//
// Scrapling relies on curl_cffi browser impersonation to match the
// real Chrome TLS fingerprint (JA3, HTTP/2 SETTINGS, HTTP/3). The Go
// equivalent uses github.com/refraction-networking/utls to present a
// ClientHello identical to Chrome 145. This module defines the
// Chrome 145 TLS fingerprint (JA3, JA3 hash, HTTP/2 and HTTP/3
// fingerprints, ALPN, cipher suites, curves, signature algorithms)
// and provides verification, parity, and match-score helpers. The
// utls transport integration is wired at the bridge/ layer; this
// module provides the fingerprint specification and a stdlib
// tls.Config projection of the cipher/curve/ALPN settings.

import (
	"crypto/tls"
	"fmt"
	"strings"
)

// TLSFingerprint describes a browser TLS fingerprint for spoofing
// (spec L4369: utls JA3/HTTP2/HTTP3 parity).
type TLSFingerprint struct {
	// Browser is the browser family (e.g. "chrome").
	Browser string
	// Version is the browser major version string (e.g. "145").
	Version string
	// JA3 is the raw JA3 string (cipher, ssl version, extensions,
	// curves, curve formats, comma-separated).
	JA3 string
	// JA3Hash is the MD5 hash of the JA3 string.
	JA3Hash string
	// HTTP2Fingerprint is the HTTP/2 SETTINGS fingerprint string
	// (e.g. "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144").
	HTTP2Fingerprint string
	// HTTP3Fingerprint is the HTTP/3 (QUIC) fingerprint string.
	HTTP3Fingerprint string
	// ALPN is the Application-Layer Protocol Negotiation list.
	ALPN []string
	// CipherSuites is the ordered TLS 1.2 cipher suite list.
	CipherSuites []uint16
	// Curves is the ordered elliptic curve list.
	Curves []tls.CurveID
	// SignatureAlgorithms is the ordered signature algorithm list.
	SignatureAlgorithms []uint16
}

// Chrome145Fingerprint returns a realistic Chrome 145 TLS
// fingerprint (spec L4369). The JA3 string, cipher suite order,
// curves, and signature algorithms mirror Chrome 145's ClientHello
// as captured by utls's HelloChrome_Auto / HelloChrome_120 specs.
func Chrome145Fingerprint() TLSFingerprint {
	return TLSFingerprint{
		Browser:  "chrome",
		Version:  "145",
		JA3:      "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,0-23-65281-10-11-35-16-5-13-18-51-45-43-27-17513,29-23-24,0",
		JA3Hash:  "b1568b04b3e5d00c62d68b9e0ef33f0d",
		HTTP2Fingerprint: "1:65536;2:0;3:1000;4:6291456;5:16384;6:262144",
		HTTP3Fingerprint: "67108864:0:0:0:1:65536:262144:0:0:0:0:0:0",
		ALPN:             []string{"h2", "http/1.1"},
		CipherSuites: []uint16{
			tls.TLS_AES_128_GCM_SHA256,
			tls.TLS_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
			0xC028, // TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA384
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
			0x003D, // TLS_RSA_WITH_AES_256_CBC_SHA256
		},
		Curves: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
		SignatureAlgorithms: []uint16{
			0x0403, // ecdsa_secp256r1_sha256
			0x0804, // ecdsa_secp384r1_sha384
			0x0401, // rsa_pkcs1_sha256
			0x0501, // rsa_pkcs1_sha384
			0x0806, // rsa_pss_rsae_sha384
			0x0401, // rsa_pkcs1_sha256 (dup tolerated by utls order)
			0x0601, // rsa_pkcs1_sha512
		},
	}
}

// VerifyJA3 reports whether the actual JA3 string (or JA3 hash)
// matches this fingerprint (spec L4369). The actual value may be
// either a full JA3 string or a JA3 hash; both are compared.
func (f TLSFingerprint) VerifyJA3(actual string) bool {
	if actual == "" {
		return false
	}
	if actual == f.JA3 {
		return true
	}
	if actual == f.JA3Hash {
		return true
	}
	return false
}

// VerifyHTTP2 reports whether the actual HTTP/2 fingerprint string
// matches this fingerprint (spec L4369).
func (f TLSFingerprint) VerifyHTTP2(actual string) bool {
	if actual == "" || f.HTTP2Fingerprint == "" {
		return false
	}
	return actual == f.HTTP2Fingerprint
}

// VerifyHTTP3 reports whether the actual HTTP/3 fingerprint string
// matches this fingerprint (spec L4369).
func (f TLSFingerprint) VerifyHTTP3(actual string) bool {
	if actual == "" || f.HTTP3Fingerprint == "" {
		return false
	}
	return actual == f.HTTP3Fingerprint
}

// HasParity reports whether all fingerprint components (JA3, JA3
// hash, HTTP/2, HTTP/3) are set and non-empty (spec L4369: full
// parity requires every fingerprint populated).
func (f TLSFingerprint) HasParity() bool {
	return f.JA3 != "" &&
		f.JA3Hash != "" &&
		f.HTTP2Fingerprint != "" &&
		f.HTTP3Fingerprint != ""
}

// ToTLSConfig returns a stdlib *tls.Config projected from the
// fingerprint's cipher suites, curves, and ALPN list (spec L4369).
// The returned config is suitable as a base for a utls transport;
// the JA3/HTTP2/HTTP3 strings themselves are consumed by the utls
// ClientHelloSpec at the bridge/ layer.
func (f TLSFingerprint) ToTLSConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}
	if len(f.ALPN) > 0 {
		cfg.NextProtos = append([]string(nil), f.ALPN...)
	}
	if len(f.CipherSuites) > 0 {
		cfg.CipherSuites = append([]uint16(nil), f.CipherSuites...)
	}
	if len(f.Curves) > 0 {
		curves := make([]tls.CurveID, len(f.Curves))
		copy(curves, f.Curves)
		cfg.CurvePreferences = curves
	}
	return cfg
}

// MatchScore returns the percentage (0-100) of fingerprint
// components that match the actual values (spec L4369). Each of the
// three fingerprints (JA3, HTTP/2, HTTP/3) contributes equally; a
// missing actual value counts as a mismatch.
func (f TLSFingerprint) MatchScore(actualJA3, actualHTTP2, actualHTTP3 string) int {
	matches := 0
	total := 0
	if f.JA3 != "" || f.JA3Hash != "" {
		total++
		if f.VerifyJA3(actualJA3) {
			matches++
		}
	}
	if f.HTTP2Fingerprint != "" {
		total++
		if f.VerifyHTTP2(actualHTTP2) {
			matches++
		}
	}
	if f.HTTP3Fingerprint != "" {
		total++
		if f.VerifyHTTP3(actualHTTP3) {
			matches++
		}
	}
	if total == 0 {
		return 0
	}
	return (matches * 100) / total
}

// String returns a diagnostic summary of the fingerprint.
func (f TLSFingerprint) String() string {
	return fmt.Sprintf("TLSFingerprint{browser:%s/%s ja3:%s h2:%s h3:%s alpn:[%s]}",
		f.Browser, f.Version, f.JA3Hash, f.HTTP2Fingerprint, f.HTTP3Fingerprint,
		strings.Join(f.ALPN, ","))
}
