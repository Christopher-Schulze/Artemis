package network

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	httpsig "github.com/remitly-oss/httpsig-go"
)

// webbotauth.go (TASK-2343, TASK-2988: WebBotAuth - cryptographic good-bot
// signature).
//
// WebBotAuth is the honest counterpart to the stealth layer: instead of
// hiding that the agent is a bot, it signs outbound HTTP requests so that
// participating origins can verify the agent's identity. It follows the IETF
// Web Bot Auth architecture (draft-meunier-web-bot-auth-architecture) on top
// of RFC 9421 HTTP Message Signatures with Ed25519 keys. The RFC 9421 signature
// base, structured-field encoding and verification come from the vetted
// github.com/remitly-oss/httpsig-go library (AGENTS.md: no custom cryptography).
//
// The signer is operator-gated: it only activates when the operator provides
// a private key and agent URL via config. When disabled, requests pass
// through unsigned (the default).
//
// Headers produced on each signed request:
//   Signature-Agent: "<agentURL>"
//   Signature-Input: sig1=("@authority" "signature-agent");created=...;expires=...;nonce=...;alg="ed25519";keyid="...";tag="web-bot-auth"
//   Signature: sig1=:<base64-signature>:
//
// Signature-Agent is a covered component, so the claimed identity cannot be
// swapped without invalidating the signature; the nonce enables replay
// rejection in WebBotAuthVerifier.

// WebBotAuthTag is the required tag parameter value for web-bot-auth.
const WebBotAuthTag = "web-bot-auth"

// WebBotAuthLabel is the signature label used in Signature-Input/Signature.
const WebBotAuthLabel = "sig1"

// webBotAuthFields are the covered components of every web-bot-auth signature.
var webBotAuthFields = []string{"@authority", "signature-agent"}

var webBotAuthMetadata = []httpsig.Metadata{httpsig.MetaCreated, httpsig.MetaExpires, httpsig.MetaNonce, httpsig.MetaAlgorithm, httpsig.MetaKeyID, httpsig.MetaTag}

// WebBotAuthConfig configures the WebBotAuth signer (operator-gated).
type WebBotAuthConfig struct {
	// Enabled gates signing. When false, Sign is a no-op.
	Enabled bool
	// AgentURL is the agent's identity URL (e.g. "https://agent.example.com"),
	// published as the covered Signature-Agent header.
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
	cfg    WebBotAuthConfig
	stats  WebBotAuthStats
	signer *httpsig.Signer
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
	agent, err := url.Parse(cfg.AgentURL)
	if err != nil || agent.Scheme != "https" || agent.Host == "" {
		return nil, fmt.Errorf("webbotauth: AgentURL must be an absolute https URL")
	}
	signer, err := httpsig.NewSigner(httpsig.SigningProfile{
		Algorithm:       httpsig.Algo_ED25519,
		Fields:          httpsig.Fields(webBotAuthFields...),
		Metadata:        webBotAuthMetadata,
		Label:           WebBotAuthLabel,
		ExpiresDuration: cfg.MaxExpiry,
		Nonce:           httpsig.NonceRandom32,
	}, httpsig.SigningKey{Key: cfg.PrivateKey, MetaKeyID: cfg.KeyID, MetaTag: WebBotAuthTag})
	if err != nil {
		return nil, fmt.Errorf("webbotauth: signer: %w", err)
	}
	return &WebBotAuth{cfg: cfg, signer: signer}, nil
}

// Sign signs an outbound HTTP request in place by adding the covered
// Signature-Agent header and the RFC 9421 Signature-Input and Signature
// headers. If the signer is disabled, the request is returned unmodified.
func (w *WebBotAuth) Sign(req *http.Request) error {
	w.stats.Total.Add(1)
	if !w.cfg.Enabled {
		w.stats.Skipped.Add(1)
		return nil
	}
	if req == nil || req.URL == nil {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: nil request")
	}
	if req.Host == "" && req.URL.Host == "" {
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: cannot determine @authority (empty Host)")
	}
	if req.Host == "" {
		// Redirect hops arrive with an empty Host; sign the authority the
		// transport actually sends.
		req.Host = req.URL.Host
	}
	if req.Header == nil {
		req.Header = http.Header{}
	}
	req.Header.Set("Signature-Agent", fmt.Sprintf("%q", w.cfg.AgentURL))
	if err := w.signer.Sign(req); err != nil {
		w.stats.Errors.Add(1)
		return fmt.Errorf("webbotauth: sign: %w", err)
	}
	w.stats.Signed.Add(1)
	return nil
}

// SignRequest signs a network.Request for method and targetURL by setting the
// three signature headers in r.Headers.
func (w *WebBotAuth) SignRequest(r *Request, method, targetURL string) error {
	if !w.cfg.Enabled {
		w.stats.Total.Add(1)
		w.stats.Skipped.Add(1)
		return nil
	}
	if r == nil {
		w.stats.Total.Add(1)
		w.stats.Errors.Add(1)
		return errors.New("webbotauth: nil request")
	}
	probe, err := http.NewRequest(method, targetURL, nil)
	if err != nil {
		w.stats.Total.Add(1)
		w.stats.Errors.Add(1)
		return fmt.Errorf("webbotauth: build request: %w", err)
	}
	if err := w.Sign(probe); err != nil {
		return err
	}
	if r.Headers == nil {
		r.Headers = http.Header{}
	}
	for _, name := range []string{"Signature-Agent", "Signature-Input", "Signature"} {
		r.Headers.Set(name, probe.Header.Get(name))
	}
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

// webBotAuthKey resolves exactly one trusted Ed25519 key.
type webBotAuthKey struct {
	spec httpsig.KeySpec
}

func (k webBotAuthKey) FetchByKeyID(_ context.Context, _ http.Header, keyID string) (httpsig.KeySpecer, error) {
	if keyID != k.spec.KeyID {
		return nil, fmt.Errorf("webbotauth: unknown keyid %q", keyID)
	}
	return k.spec, nil
}

func (k webBotAuthKey) Fetch(context.Context, http.Header, httpsig.MetadataProvider) (httpsig.KeySpecer, error) {
	return nil, errors.New("webbotauth: keyid required")
}

// WebBotAuthVerifier verifies web-bot-auth signatures for one agent key and
// rejects replayed nonces until their signature expires. It is safe for
// concurrent use; the nonce memory is bounded.
type WebBotAuthVerifier struct {
	verifier  *httpsig.Verifier
	maxNonces int
	now       func() time.Time
	mu        sync.Mutex
	nonces    map[string]time.Time
}

const defaultWebBotAuthNonces = 100_000

// NewWebBotAuthVerifier builds a verifier for pub. maxNonces <= 0 selects
// 100,000 remembered nonces.
func NewWebBotAuthVerifier(pub ed25519.PublicKey, maxNonces int) (*WebBotAuthVerifier, error) {
	if len(pub) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("webbotauth: public key len = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	keyID, err := computeKeyID(pub)
	if err != nil {
		return nil, err
	}
	verifier, err := httpsig.NewVerifier(webBotAuthKey{spec: httpsig.KeySpec{KeyID: keyID, Algo: httpsig.Algo_ED25519, PubKey: pub}}, httpsig.VerifyProfile{
		SignatureLabel:       WebBotAuthLabel,
		RequiredFields:       httpsig.Fields(webBotAuthFields...),
		RequiredMetadata:     webBotAuthMetadata,
		AllowedAlgorithms:    []httpsig.Algorithm{httpsig.Algo_ED25519},
		CreatedValidDuration: 24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("webbotauth: verifier: %w", err)
	}
	if maxNonces <= 0 {
		maxNonces = defaultWebBotAuthNonces
	}
	return &WebBotAuthVerifier{verifier: verifier, maxNonces: maxNonces, now: time.Now, nonces: make(map[string]time.Time)}, nil
}

// Verify checks signature, covered components, metadata, freshness, the
// web-bot-auth tag and nonce uniqueness. Returns nil only for a valid,
// first-seen signature.
func (v *WebBotAuthVerifier) Verify(req *http.Request) error {
	if v == nil || req == nil {
		return errors.New("webbotauth: verifier and request required")
	}
	if req.Header.Get("Signature-Input") == "" || req.Header.Get("Signature") == "" {
		return errors.New("webbotauth: missing Signature-Input or Signature header")
	}
	result, err := v.verifier.Verify(req)
	if err != nil {
		return fmt.Errorf("webbotauth: signature verification failed: %w", err)
	}
	if !result.Verified {
		return errors.New("webbotauth: signature verification failed")
	}
	tag, err := result.Tag()
	if err != nil || tag != WebBotAuthTag {
		return fmt.Errorf("webbotauth: tag must be %q", WebBotAuthTag)
	}
	nonce, err := result.Nonce()
	if err != nil || nonce == "" {
		return errors.New("webbotauth: nonce required")
	}
	expires, err := result.Expires()
	if err != nil {
		return errors.New("webbotauth: expires required")
	}
	return v.admitNonce(nonce, time.Unix(int64(expires), 0))
}

func (v *WebBotAuthVerifier) admitNonce(nonce string, expires time.Time) error {
	now := v.now()
	v.mu.Lock()
	defer v.mu.Unlock()
	if seenUntil, seen := v.nonces[nonce]; seen && now.Before(seenUntil) {
		return errors.New("webbotauth: replayed nonce")
	}
	if len(v.nonces) >= v.maxNonces {
		for key, until := range v.nonces {
			if !now.Before(until) {
				delete(v.nonces, key)
			}
		}
		if len(v.nonces) >= v.maxNonces {
			return errors.New("webbotauth: nonce memory exhausted; rejecting to stay replay-safe")
		}
	}
	v.nonces[nonce] = expires
	return nil
}

// VerifyWebBotAuthSignature verifies one web-bot-auth signature on req with
// pub without replay memory. Servers that accept repeated requests should use
// WebBotAuthVerifier to also reject replayed nonces.
func VerifyWebBotAuthSignature(req *http.Request, pub ed25519.PublicKey) error {
	verifier, err := NewWebBotAuthVerifier(pub, 1)
	if err != nil {
		return err
	}
	return verifier.Verify(req)
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

// CloseIdleConnections forwards lifecycle teardown to the wrapped transport.
func (m *WebBotAuthMiddleware) CloseIdleConnections() {
	if closer, ok := m.Transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
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
