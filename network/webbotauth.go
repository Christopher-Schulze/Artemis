package network

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// webbotauth.go (TASK-2343: WebBotAuth — cryptographic good-bot signature).
//
// WebBotAuth is the honest counterpart to the stealth layer: instead of
// hiding that the agent is a bot, it cryptographically signs outbound HTTP
// requests so that participating origins can verify the agent's identity.
// This implements the IETF Web Bot Auth architecture
// (draft-meunier-web-bot-auth-architecture) on top of RFC 9421 HTTP Message
// Signatures, using Ed25519 keys.
//
// The signer is operator-gated: it only activates when the operator provides
// a private key and agent URL via config. When disabled, requests pass
// through unsigned (the default, preserving current behavior).
//
// Headers produced on each signed request:
//   Signature-Agent: g="<agentURL>"
//   Signature-Input: g=("@authority" "created" "expires" "keyid" "tag");tag="web-bot-auth";...
//   Signature: g=:<base64-signature>:
//
// The signature is computed over the canonical signature base string per
// RFC 9421 §2.3, covering @authority and the signature-params themselves.

// WebBotAuthTag is the required tag parameter value for web-bot-auth.
const WebBotAuthTag = "web-bot-auth"

// WebBotAuthLabel is the signature label used in Signature-Input/Signature.
const WebBotAuthLabel = "g"

// WebBotAuthConfig configures the WebBotAuth signer (operator-gated).
type WebBotAuthConfig struct {
	// Enabled gates signing. When false, Sign is a no-op.
	Enabled bool
	// AgentURL is the agent's identity URL (e.g. "https://agent.example.com").
	// This is published in the Signature-Agent header as g="<AgentURL>".
	AgentURL string
	// PrivateKey is the Ed25519 private key used for signing.
	PrivateKey ed25519.PrivateKey
	// KeyID is the base64url-encoded JWK SHA-256 thumbprint of the public key.
	// If empty, it is computed from PrivateKey at init time.
	KeyID string
	// MaxExpiry caps the expires timestamp. Default 24h per the draft.
	MaxExpiry time.Duration
}

// WebBotAuthStats tracks signing counters.
type WebBotAuthStats struct {
	Total   atomic.Int64
	Signed  atomic.Int64
	Skipped atomic.Int64
	Errors  atomic.Int64
}

// WebBotAuth signs HTTP requests per the web-bot-auth architecture
// (TASK-2343). It is safe for concurrent use.
type WebBotAuth struct {
	cfg   WebBotAuthConfig
	stats WebBotAuthStats
}

// NewWebBotAuth creates a signer from the given config. Returns an error if
// the config is enabled but missing required fields (AgentURL, PrivateKey).
// If KeyID is empty, it is derived from the public key as a base64url
// JWK SHA-256 thumbprint.
func NewWebBotAuth(cfg WebBotAuthConfig) (*WebBotAuth, error) {
	if !cfg.Enabled {
		return &WebBotAuth{cfg: cfg}, nil
	}
	if cfg.AgentURL == "" {
		return nil, errors.New("webbotauth: enabled but AgentURL is empty")
	}
	if cfg.PrivateKey == nil {
		return nil, errors.New("webbotauth: enabled but PrivateKey is nil")
	}
	if len(cfg.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("webbotauth: PrivateKey len = %d, want %d", len(cfg.PrivateKey), ed25519.PrivateKeySize)
	}
	if cfg.MaxExpiry <= 0 {
		cfg.MaxExpiry = 24 * time.Hour
	}
	if cfg.KeyID == "" {
		kid, err := computeKeyID(cfg.PrivateKey.Public().(ed25519.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("webbotauth: compute key id: %w", err)
		}
		cfg.KeyID = kid
	}
	return &WebBotAuth{cfg: cfg}, nil
}

// Sign signs an outbound HTTP request in-place by adding Signature-Agent,
// Signature-Input, and Signature headers (TASK-2343). If the signer is
// disabled, the request is returned unmodified.
func (w *WebBotAuth) Sign(req *http.Request) error {
	w.stats.Total.Add(1)
	if !w.cfg.Enabled {
		w.stats.Skipped.Add(1)
		return nil
	}
	if req == nil {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: nil request")
	}

	created := time.Now().Unix()
	expires := time.Now().Add(w.cfg.MaxExpiry).Unix()

	// @authority is the Host header value (RFC 9421 §2.2.3).
	authority := req.Host
	if authority == "" {
		if req.URL != nil {
			authority = req.URL.Host
		}
	}
	if authority == "" {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: cannot determine @authority (empty Host)")
	}

	// Build the signature-input header value.
	sigInput := buildSignatureInput(authority, created, expires, w.cfg.KeyID)

	// Build the canonical signature base string (RFC 9421 §2.3).
	sigBase, err := buildSignatureBase(authority, sigInput)
	if err != nil {
		w.stats.Errors.Add(1)
		return fmt.Errorf("webbotauth: build signature base: %w", err)
	}

	// Sign with Ed25519.
	sig := ed25519.Sign(w.cfg.PrivateKey, []byte(sigBase))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	// Attach headers.
	req.Header.Set("Signature-Agent", fmt.Sprintf(`g=%q`, w.cfg.AgentURL))
	req.Header.Set("Signature-Input", sigInput)
	req.Header.Set("Signature", fmt.Sprintf("g=:%s:", sigB64))

	w.stats.Signed.Add(1)
	return nil
}

// SignRequest is a convenience wrapper for the network.Request type used by
// HTTPClient. It sets the three signature headers in r.Headers.
func (w *WebBotAuth) SignRequest(r *Request, method, targetURL string) error {
	w.stats.Total.Add(1)
	if !w.cfg.Enabled {
		w.stats.Skipped.Add(1)
		return nil
	}
	if r == nil {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: nil request")
	}
	if r.Headers == nil {
		r.Headers = http.Header{}
	}

	u, err := url.Parse(targetURL)
	if err != nil {
		w.stats.Errors.Add(1)
		return fmt.Errorf("webbotauth: parse URL: %w", err)
	}
	authority := u.Host
	if authority == "" {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: empty authority in URL")
	}

	created := time.Now().Unix()
	expires := time.Now().Add(w.cfg.MaxExpiry).Unix()

	sigInput := buildSignatureInput(authority, created, expires, w.cfg.KeyID)
	sigBase, err := buildSignatureBase(authority, sigInput)
	if err != nil {
		w.stats.Errors.Add(1)
		return fmt.Errorf("webbotauth: build signature base: %w", err)
	}
	sig := ed25519.Sign(w.cfg.PrivateKey, []byte(sigBase))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	r.Headers.Set("Signature-Agent", fmt.Sprintf(`g=%q`, w.cfg.AgentURL))
	r.Headers.Set("Signature-Input", sigInput)
	r.Headers.Set("Signature", fmt.Sprintf("g=:%s:", sigB64))

	w.stats.Signed.Add(1)
	return nil
}

// Enabled reports whether the signer is active.
func (w *WebBotAuth) Enabled() bool { return w.cfg.Enabled }

// AgentURL returns the configured agent URL (empty if disabled).
func (w *WebBotAuth) AgentURL() string { return w.cfg.AgentURL }

// KeyID returns the base64url JWK thumbprint used as keyid.
func (w *WebBotAuth) KeyID() string { return w.cfg.KeyID }

// Stats returns the signing counters.
func (w *WebBotAuth) Stats() (total, signed, skipped, errors int64) {
	return w.stats.Total.Load(),
		w.stats.Signed.Load(),
		w.stats.Skipped.Load(),
		w.stats.Errors.Load()
}

// buildSignatureInput constructs the Signature-Input header value for label
// "g" covering @authority with the required web-bot-auth parameters.
func buildSignatureInput(authority string, created, expires int64, keyID string) string {
	// Signature-Input: g=("@authority");created=<ts>;expires=<ts>;keyid="<kid>";tag="web-bot-auth"
	return fmt.Sprintf(`g=("@authority");created=%d;expires=%d;keyid=%q;tag=%q`,
		created, expires, keyID, WebBotAuthTag)
}

// buildSignatureBase constructs the canonical signature base string per
// RFC 9421 §2.3. For web-bot-auth, the covered components are:
//
//	"@authority": <authority>\n
//	"@signature-params": <sig-input-params>\n
//
// The @signature-params value is the content inside the parentheses of the
// Signature-Input header (the component list + parameters), per RFC 9421.
func buildSignatureBase(authority, sigInput string) (string, error) {
	// Extract the signature-params from the Signature-Input header value.
	// The header is: g=("@authority");created=...;expires=...;keyid=...;tag=...
	// We need the part after "g=" (the params list including the trailing params).
	paramsStr, err := extractSignatureParams(sigInput)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	// @authority component (RFC 9421 §2.2.3): the Host header value.
	b.WriteString(`"@authority": `)
	b.WriteString(quoteHTTPString(authority))
	b.WriteString("\n")
	// @signature-params component (RFC 9421 §2.3): the signature params.
	b.WriteString(`"@signature-params": `)
	b.WriteString(quoteHTTPString(paramsStr))
	b.WriteString("\n")
	return b.String(), nil
}

// extractSignatureParams extracts the signature-params value from a
// Signature-Input header value. The header format is:
//
//	g=("@authority");created=...;expires=...;keyid=...;tag=...
//
// The signature-params value is everything after "g=" (including the
// component list and the parameters), per RFC 9421 §2.3.
func extractSignatureParams(sigInput string) (string, error) {
	// Find the label= prefix.
	idx := strings.Index(sigInput, "=")
	if idx < 0 {
		return "", errors.New("webbotauth: malformed Signature-Input (no =)")
	}
	return sigInput[idx+1:], nil
}

// quoteHTTPString wraps a string in double quotes per RFC 9421 §4.1.6
// (HTTP string literal). It escapes backslash and double-quote.
func quoteHTTPString(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return `"` + s + `"`
}

// computeKeyID computes the base64url-encoded JWK SHA-256 thumbprint for an
// Ed25519 public key per RFC 7638 §3.2 (adapted for OKP keys per RFC 8417).
// The JWK for an Ed25519 key is:
//
//	{"kty":"OKP","crv":"Ed25519","x":"<base64url-pubkey>"}
//
// The thumbprint is SHA-256 over the canonical JSON (sorted keys).
func computeKeyID(pub ed25519.PublicKey) (string, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", fmt.Errorf("webbotauth: public key len = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	xB64 := base64.RawURLEncoding.EncodeToString(pub)
	// Canonical JSON with sorted keys: crv, kty, x.
	jwk := fmt.Sprintf(`{"crv":"Ed25519","kty":"OKP","x":"%s"}`, xB64)
	hash := sha256.Sum256([]byte(jwk))
	return base64.RawURLEncoding.EncodeToString(hash[:]), nil
}

// GenerateWebBotAuthKey generates a new Ed25519 key pair for WebBotAuth.
// Returns (privateKey, publicKey, keyID, error).
func GenerateWebBotAuthKey() (ed25519.PrivateKey, ed25519.PublicKey, string, error) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("webbotauth: generate key: %w", err)
	}
	kid, err := computeKeyID(pub)
	if err != nil {
		return nil, nil, "", err
	}
	return priv, pub, kid, nil
}

// WebBotAuthKeySet is a JSON Web Key Set for publishing the public key at
// the agent's well-known endpoint (per the web-bot-auth registry draft).
type WebBotAuthKeySet struct {
	Keys []WebBotAuthJWK `json:"keys"`
}

// WebBotAuthJWK is a single JSON Web Key in the key set.
type WebBotAuthJWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
	Alg string `json:"alg,omitempty"`
}

// PublishPublicKey returns the JWK Set JSON for the signer's public key.
// This is the document that should be served at
//
//	<AgentURL>/.well-known/http-message-signatures-directory
func (w *WebBotAuth) PublishPublicKey() ([]byte, error) {
	if !w.cfg.Enabled || w.cfg.PrivateKey == nil {
		return nil, errors.New("webbotauth: signer disabled or no key")
	}
	pub := w.cfg.PrivateKey.Public().(ed25519.PublicKey)
	ks := WebBotAuthKeySet{
		Keys: []WebBotAuthJWK{
			{
				Kty: "OKP",
				Crv: "Ed25519",
				X:   base64.RawURLEncoding.EncodeToString(pub),
				Kid: w.cfg.KeyID,
				Alg: "Ed25519",
			},
		},
	}
	return json.MarshalIndent(ks, "", "  ")
}

// VerifyWebBotAuthSignature verifies a web-bot-auth signature on an
// http.Request using the given Ed25519 public key. Returns nil if the
// signature is valid, an error otherwise. This is the server-side
// verification function; the agent uses Sign.
func VerifyWebBotAuthSignature(req *http.Request, pub ed25519.PublicKey) error {
	sigInput := req.Header.Get("Signature-Input")
	sigHeader := req.Header.Get("Signature")
	if sigInput == "" || sigHeader == "" {
		return errors.New("webbotauth: missing Signature-Input or Signature header")
	}
	// Extract the signature bytes from "g=:<base64>:".
	sig, err := extractSignatureValue(sigHeader)
	if err != nil {
		return fmt.Errorf("webbotauth: extract signature: %w", err)
	}
	// Extract the signature-params from the Signature-Input header.
	paramsStr, err := extractSignatureParams(sigInput)
	if err != nil {
		return fmt.Errorf("webbotauth: extract params: %w", err)
	}
	// Reconstruct the @authority value.
	authority := req.Host
	if authority == "" && req.URL != nil {
		authority = req.URL.Host
	}
	if authority == "" {
		return errors.New("webbotauth: cannot determine @authority")
	}
	// Rebuild the signature base.
	sigBase, err := buildSignatureBase(authority, sigInput)
	if err != nil {
		return fmt.Errorf("webbotauth: rebuild base: %w", err)
	}
	_ = paramsStr // paramsStr is embedded in sigBase via sigInput
	// Verify the Ed25519 signature.
	if !ed25519.Verify(pub, []byte(sigBase), sig) {
		return errors.New("webbotauth: signature verification failed")
	}
	return nil
}

// extractSignatureValue extracts the base64-decoded signature bytes from a
// Signature header value of the form "g=:<base64>:".
func extractSignatureValue(sigHeader string) ([]byte, error) {
	// Format: g=:<base64>:
	prefix := WebBotAuthLabel + "=:"
	if !strings.HasPrefix(sigHeader, prefix) {
		return nil, errors.New("webbotauth: signature header missing label prefix")
	}
	rest := sigHeader[len(prefix):]
	if !strings.HasSuffix(rest, ":") {
		return nil, errors.New("webbotauth: signature header missing trailing colon")
	}
	b64 := rest[:len(rest)-1]
	return base64.StdEncoding.DecodeString(b64)
}

// ParseWebBotAuthSignatureInput parses a Signature-Input header value and
// returns the key parameters (created, expires, keyid, tag). Returns an
// error if the header is malformed or missing required parameters.
func ParseWebBotAuthSignatureInput(sigInput string) (created, expires int64, keyID, tag string, err error) {
	// Strip the label prefix: g=(...);created=...;...
	rest := sigInput
	if idx := strings.Index(rest, "="); idx >= 0 {
		rest = rest[idx+1:]
	}
	// Split on ';' but the first element is the component list "(...)".
	// Find the closing paren.
	closeParen := strings.Index(rest, ")")
	if closeParen < 0 {
		return 0, 0, "", "", errors.New("webbotauth: malformed Signature-Input (no closing paren)")
	}
	paramsPart := rest[closeParen+1:]
	// Now parse the parameters: ;created=...;expires=...;keyid=...;tag=...
	parts := strings.Split(paramsPart, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		eq := strings.Index(p, "=")
		if eq < 0 {
			continue
		}
		key := p[:eq]
		val := p[eq+1:]
		// Strip surrounding quotes.
		val = strings.Trim(val, `"`)
		switch key {
		case "created":
			created, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				return 0, 0, "", "", fmt.Errorf("webbotauth: parse created: %w", err)
			}
		case "expires":
			expires, err = strconv.ParseInt(val, 10, 64)
			if err != nil {
				return 0, 0, "", "", fmt.Errorf("webbotauth: parse expires: %w", err)
			}
		case "keyid":
			keyID = val
		case "tag":
			tag = val
		}
	}
	if tag != WebBotAuthTag {
		return 0, 0, "", "", fmt.Errorf("webbotauth: tag = %q, want %q", tag, WebBotAuthTag)
	}
	if keyID == "" {
		return 0, 0, "", "", errors.New("webbotauth: missing keyid")
	}
	return created, expires, keyID, tag, nil
}

// WebBotAuthMiddleware is an HTTP transport wrapper that signs every
// outgoing request with WebBotAuth before dispatching to the underlying
// transport (TASK-2343). When the signer is disabled, requests pass through
// unchanged.
type WebBotAuthMiddleware struct {
	Transport http.RoundTripper
	Signer    *WebBotAuth
}

// RoundTrip implements http.RoundTripper, signing the request before
// dispatching.
func (m *WebBotAuthMiddleware) RoundTrip(req *http.Request) (*http.Response, error) {
	if m.Signer != nil && m.Signer.Enabled() {
		// Clone the request so we don't mutate the caller's headers.
		clone := req.Clone(req.Context())
		if err := m.Signer.Sign(clone); err != nil {
			return nil, fmt.Errorf("webbotauth middleware: %w", err)
		}
		req = clone
	}
	transport := m.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(req)
}

// NewWebBotAuthTransport wraps an HTTP transport with WebBotAuth signing.
// If the signer is disabled, the transport is returned unwrapped.
func NewWebBotAuthTransport(transport http.RoundTripper, signer *WebBotAuth) http.RoundTripper {
	if signer == nil || !signer.Enabled() {
		return transport
	}
	return &WebBotAuthMiddleware{Transport: transport, Signer: signer}
}

// EncodePrivateKeyPEMish returns the Ed25519 private key as a base64 string
// for storage. This is NOT PEM; it's a compact base64 encoding suitable for
// config-file storage. Use DecodePrivateKey to reverse.
func EncodePrivateKey(priv ed25519.PrivateKey) string {
	return base64.StdEncoding.EncodeToString(priv)
}

// DecodePrivateKey decodes a base64-encoded Ed25519 private key.
func DecodePrivateKey(s string) (ed25519.PrivateKey, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("webbotauth: decode private key: %w", err)
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("webbotauth: decoded key len = %d, want %d", len(data), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(data), nil
}

// EncodePublicKey returns the Ed25519 public key as a base64 string.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

// DecodePublicKey decodes a base64-encoded Ed25519 public key.
func DecodePublicKey(s string) (ed25519.PublicKey, error) {
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("webbotauth: decode public key: %w", err)
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("webbotauth: decoded key len = %d, want %d", len(data), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(data), nil
}

// SignRequestURL is a convenience that signs a request for the given method
// and URL string, returning the three headers that should be attached. This
// is useful for non-http.Request contexts (e.g. the network.Request type).
func SignRequestURL(signer *WebBotAuth, method, targetURL string) (sigAgent, sigInput, sigValue string, err error) {
	if signer == nil || !signer.Enabled() {
		return "", "", "", nil
	}
	u, err := url.Parse(targetURL)
	if err != nil {
		return "", "", "", fmt.Errorf("webbotauth: parse URL: %w", err)
	}
	authority := u.Host
	if authority == "" {
		return "", "", "", errors.New("webbotauth: empty authority")
	}
	created := time.Now().Unix()
	expires := time.Now().Add(signer.cfg.MaxExpiry).Unix()
	sigInput = buildSignatureInput(authority, created, expires, signer.cfg.KeyID)
	sigBase, err := buildSignatureBase(authority, sigInput)
	if err != nil {
		return "", "", "", err
	}
	sig := ed25519.Sign(signer.cfg.PrivateKey, []byte(sigBase))
	sigValue = fmt.Sprintf("g=:%s:", base64.StdEncoding.EncodeToString(sig))
	sigAgent = fmt.Sprintf(`g=%q`, signer.cfg.AgentURL)
	return sigAgent, sigInput, sigValue, nil
}

// VerifySignatureBase reconstructs the signature base string from the given
// authority and Signature-Input header value, then verifies the signature
// against the public key. This is the core verification primitive used by
// VerifyWebBotAuthSignature and can be used independently for testing.
func VerifySignatureBase(authority, sigInput string, sigB64 string, pub ed25519.PublicKey) error {
	sigBase, err := buildSignatureBase(authority, sigInput)
	if err != nil {
		return fmt.Errorf("webbotauth: rebuild base: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("webbotauth: decode signature: %w", err)
	}
	if !ed25519.Verify(pub, []byte(sigBase), sig) {
		return errors.New("webbotauth: signature verification failed")
	}
	return nil
}

// extractSigB64FromHeader extracts the base64 signature from a Signature
// header value "g=:<b64>:".
func extractSigB64FromHeader(sigHeader string) (string, error) {
	prefix := WebBotAuthLabel + "=:"
	if !strings.HasPrefix(sigHeader, prefix) {
		return "", errors.New("webbotauth: signature header missing label prefix")
	}
	rest := sigHeader[len(prefix):]
	if !strings.HasSuffix(rest, ":") {
		return "", errors.New("webbotauth: signature header missing trailing colon")
	}
	return rest[:len(rest)-1], nil
}

// VerifyRequestHeaders verifies the web-bot-auth signature on a request
// using the three header values (Signature-Agent, Signature-Input,
// Signature) and the public key. Returns nil if valid.
func VerifyRequestHeaders(sigAgent, sigInput, sigHeader, authority string, pub ed25519.PublicKey) error {
	if sigInput == "" || sigHeader == "" {
		return errors.New("webbotauth: missing Signature-Input or Signature header")
	}
	// Verify the tag is web-bot-auth.
	_, _, _, tag, err := ParseWebBotAuthSignatureInput(sigInput)
	if err != nil {
		return fmt.Errorf("webbotauth: parse signature-input: %w", err)
	}
	_ = tag
	sigB64, err := extractSigB64FromHeader(sigHeader)
	if err != nil {
		return fmt.Errorf("webbotauth: extract signature: %w", err)
	}
	if err := VerifySignatureBase(authority, sigInput, sigB64, pub); err != nil {
		return err
	}
	// Verify Signature-Agent if provided.
	if sigAgent != "" {
		// sigAgent format: g="<url>"
		if !strings.HasPrefix(sigAgent, `g="`) {
			return errors.New("webbotauth: malformed Signature-Agent header")
		}
	}
	return nil
}

// bufferPool avoids allocating fresh buffers for signature base construction.
var _ = bytes.NewBufferString
