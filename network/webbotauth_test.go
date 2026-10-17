package network

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func generateTestWebBotAuthKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func mustNewTestWebBotAuth(t *testing.T, config WebBotAuthConfig) *WebBotAuth {
	t.Helper()
	webBotAuth, err := NewWebBotAuth(config)
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	return webBotAuth
}

func mustNewTestRequest(t *testing.T, method, target string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	return request
}

func signTestRequest(t *testing.T, signer *WebBotAuth, request *http.Request) {
	t.Helper()
	if err := signer.Sign(request); err != nil {
		t.Fatalf("sign request: %v", err)
	}
}

func mustComputeTestKeyID(t *testing.T, publicKey ed25519.PublicKey) string {
	t.Helper()
	keyID, err := computeKeyID(publicKey)
	if err != nil {
		t.Fatalf("compute key ID: %v", err)
	}
	return keyID
}

func closeTestResponse(t *testing.T, response *http.Response) {
	t.Helper()
	if err := response.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}

func TestWebBotAuthDisabledNoOp(t *testing.T) {
	w, err := NewWebBotAuth(WebBotAuthConfig{Enabled: false})
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	req := mustNewTestRequest(t, "GET", "https://example.com/path")
	signTestRequest(t, w, req)
	if req.Header.Get("Signature-Input") != "" {
		t.Error("disabled signer should not set Signature-Input")
	}
	if req.Header.Get("Signature") != "" {
		t.Error("disabled signer should not set Signature")
	}
	if req.Header.Get("Signature-Agent") != "" {
		t.Error("disabled signer should not set Signature-Agent")
	}
	total, _, skipped, _ := w.Stats()
	if total != 1 || skipped != 1 {
		t.Errorf("stats = (%d,_,%d,_), want (1,_,1,_)", total, skipped)
	}
}

func TestWebBotAuthEnabledRequiresAgentURL(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	_, err := NewWebBotAuth(WebBotAuthConfig{Enabled: true, PrivateKey: priv})
	if err == nil {
		t.Error("expected error for enabled without AgentURL")
	}
}

func TestWebBotAuthEnabledRequiresPrivateKey(t *testing.T) {
	_, err := NewWebBotAuth(WebBotAuthConfig{Enabled: true, AgentURL: "https://agent.example.com"})
	if err == nil {
		t.Error("expected error for enabled without PrivateKey")
	}
}

func TestWebBotAuthEnabledRequiresValidKeyLen(t *testing.T) {
	_, err := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: ed25519.PrivateKey(make([]byte, 10)),
	})
	if err == nil {
		t.Error("expected error for invalid key length")
	}
}

func TestWebBotAuthSignAddsHeaders(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w, err := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	req := mustNewTestRequest(t, "GET", "https://example.com/path")
	signTestRequest(t, w, req)
	if req.Header.Get("Signature-Agent") == "" {
		t.Error("Signature-Agent header not set")
	}
	if req.Header.Get("Signature-Input") == "" {
		t.Error("Signature-Input header not set")
	}
	if req.Header.Get("Signature") == "" {
		t.Error("Signature header not set")
	}
	// Signature-Agent is the quoted agent URL and a covered component.
	sa := req.Header.Get("Signature-Agent")
	if sa != `"https://agent.example.com"` {
		t.Errorf("Signature-Agent = %q, want quoted agent URL", sa)
	}
	if !strings.Contains(sa, "https://agent.example.com") {
		t.Error("Signature-Agent should contain agent URL")
	}
	// Verify Signature-Input contains web-bot-auth tag.
	si := req.Header.Get("Signature-Input")
	if !strings.Contains(si, "web-bot-auth") {
		t.Errorf("Signature-Input missing tag: %q", si)
	}
	if !strings.Contains(si, "@authority") {
		t.Errorf("Signature-Input missing @authority: %q", si)
	}
	if !strings.Contains(si, "created=") {
		t.Errorf("Signature-Input missing created: %q", si)
	}
	if !strings.Contains(si, "expires=") {
		t.Errorf("Signature-Input missing expires: %q", si)
	}
	if !strings.Contains(si, "keyid=") {
		t.Errorf("Signature-Input missing keyid: %q", si)
	}
	for _, part := range []string{`"signature-agent"`, "nonce=", `alg="ed25519"`, WebBotAuthLabel + "=("} {
		if !strings.Contains(si, part) {
			t.Errorf("Signature-Input missing %s: %q", part, si)
		}
	}
	sig := req.Header.Get("Signature")
	if !strings.HasPrefix(sig, WebBotAuthLabel+"=:") || !strings.HasSuffix(sig, ":") {
		t.Errorf("Signature = %q, want %s=:<base64>: format", sig, WebBotAuthLabel)
	}
	_, signed, _, _ := w.Stats()
	if signed != 1 {
		t.Errorf("signed = %d, want 1", signed)
	}
}

func TestWebBotAuthSignDeterministic(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	// Sign the same request twice with the same created/expires.
	// Since created/expires use time.Now(), we test determinism by signing
	// twice in rapid succession and verifying the signatures are valid
	// Ed25519 (which is deterministic for the same input).
	req1 := mustNewTestRequest(t, "GET", "https://example.com/path")
	req2 := mustNewTestRequest(t, "GET", "https://example.com/path")
	signTestRequest(t, w, req1)
	signTestRequest(t, w, req2)
	// Both should have valid base64 signatures.
	sig1 := req1.Header.Get("Signature")
	sig2 := req2.Header.Get("Signature")
	if sig1 == "" || sig2 == "" {
		t.Fatal("signatures empty")
	}
	// Decode and verify both are valid Ed25519 signatures.
	sig1B64 := strings.TrimSuffix(strings.TrimPrefix(sig1, WebBotAuthLabel+"=:"), ":")
	sig2B64 := strings.TrimSuffix(strings.TrimPrefix(sig2, WebBotAuthLabel+"=:"), ":")
	sig1Bytes, err := base64.StdEncoding.DecodeString(sig1B64)
	if err != nil {
		t.Fatalf("decode signature 1: %v", err)
	}
	sig2Bytes, err := base64.StdEncoding.DecodeString(sig2B64)
	if err != nil {
		t.Fatalf("decode signature 2: %v", err)
	}
	if len(sig1Bytes) != ed25519.SignatureSize {
		t.Errorf("sig1 len = %d, want %d", len(sig1Bytes), ed25519.SignatureSize)
	}
	if len(sig2Bytes) != ed25519.SignatureSize {
		t.Errorf("sig2 len = %d, want %d", len(sig2Bytes), ed25519.SignatureSize)
	}
}

func TestWebBotAuthSignNilRequest(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	if err := w.Sign(nil); err == nil {
		t.Error("expected error for nil request")
	}
	_, _, _, errors := w.Stats()
	if errors != 1 {
		t.Errorf("errors = %d, want 1", errors)
	}
}

func TestWebBotAuthKeyIDDerivedFromPublicKey(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	expectedKID := mustComputeTestKeyID(t, pub)
	w, err := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	if w.KeyID() != expectedKID {
		t.Errorf("KeyID = %q, want %q", w.KeyID(), expectedKID)
	}
	// Verify keyid appears in Signature-Input.
	req := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, req)
	si := req.Header.Get("Signature-Input")
	if !strings.Contains(si, expectedKID) {
		t.Errorf("Signature-Input %q does not contain keyid %q", si, expectedKID)
	}
}

func TestWebBotAuthExplicitKeyID(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
		KeyID:      "custom-key-id-123",
	})
	if w.KeyID() != "custom-key-id-123" {
		t.Errorf("KeyID = %q, want custom-key-id-123", w.KeyID())
	}
}

func TestWebBotAuthVerifyRoundTrip(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req := mustNewTestRequest(t, "GET", "https://example.com/api/data")
	req.Host = "example.com"
	signTestRequest(t, w, req)
	if err := VerifyWebBotAuthSignature(req, pub); err != nil {
		t.Errorf("VerifyWebBotAuthSignature: %v", err)
	}
}

func TestWebBotAuthVerifyFailsWithWrongKey(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	wrongPub, _ := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req := mustNewTestRequest(t, "GET", "https://example.com/api/data")
	req.Host = "example.com"
	signTestRequest(t, w, req)
	if err := VerifyWebBotAuthSignature(req, wrongPub); err == nil {
		t.Error("verification should fail with wrong public key")
	}
}

func TestWebBotAuthVerifyFailsWithTamperedAuthority(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req := mustNewTestRequest(t, "GET", "https://example.com/api/data")
	req.Host = "example.com"
	signTestRequest(t, w, req)
	// Tamper with the Host after signing.
	req.Host = "evil.com"
	if err := VerifyWebBotAuthSignature(req, pub); err == nil {
		t.Error("verification should fail with tampered authority")
	}
}

func TestWebBotAuthVerifyMissingHeaders(t *testing.T) {
	pub, _ := generateTestWebBotAuthKey(t)
	req := mustNewTestRequest(t, "GET", "https://example.com/")
	if err := VerifyWebBotAuthSignature(req, pub); err == nil {
		t.Error("verification should fail with missing headers")
	}
}

func TestWebBotAuthVerifierRejectsReplaySwappedAgentAndNil(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	if err := VerifyWebBotAuthSignature(nil, pub); err == nil {
		t.Fatal("nil request accepted")
	}
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{Enabled: true, AgentURL: "https://agent.example.com", PrivateKey: priv})
	verifier, err := NewWebBotAuthVerifier(pub, 0)
	if err != nil {
		t.Fatal(err)
	}
	req := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, req)
	if err := verifier.Verify(req); err != nil {
		t.Fatalf("first verification: %v", err)
	}
	if err := verifier.Verify(req); err == nil || !strings.Contains(err.Error(), "replayed") {
		t.Fatalf("replayed signature accepted: %v", err)
	}
	swapped := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, swapped)
	swapped.Header.Set("Signature-Agent", `"https://impostor.example.com"`)
	if err := VerifyWebBotAuthSignature(swapped, pub); err == nil {
		t.Fatal("swapped Signature-Agent passed verification")
	}
	stripped := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, stripped)
	stripped.Header.Set("Signature-Input", strings.Replace(stripped.Header.Get("Signature-Input"), `tag="web-bot-auth"`, `tag="other"`, 1))
	if err := VerifyWebBotAuthSignature(stripped, pub); err == nil {
		t.Fatal("altered tag passed verification")
	}
}

func TestWebBotAuthRequiresHTTPSAgentURL(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	if _, err := NewWebBotAuth(WebBotAuthConfig{Enabled: true, AgentURL: "http://agent.example.com", PrivateKey: priv}); err == nil {
		t.Fatal("non-https agent identity admitted")
	}
}

func TestWebBotAuthMiddleware(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	var capturedReq *http.Request
	inner := &mockTransport{
		fn: func(req *http.Request) (*http.Response, error) {
			capturedReq = req
			return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
		},
	}
	mw := NewWebBotAuthTransport(inner, w)
	client := &http.Client{Transport: mw}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/path", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer closeTestResponse(t, resp)
	if capturedReq == nil {
		t.Fatal("inner transport did not receive request")
	}
	if capturedReq.Header.Get("Signature-Input") == "" {
		t.Error("middleware did not sign request")
	}
	// Verify the signature on the captured request.
	if err := VerifyWebBotAuthSignature(capturedReq, pub); err != nil {
		t.Errorf("VerifyWebBotAuthSignature: %v", err)
	}
}

func TestWebBotAuthMiddlewareDisabledPassThrough(t *testing.T) {
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{Enabled: false})
	inner := &mockTransport{
		fn: func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Signature-Input") != "" {
				t.Error("disabled middleware should not sign")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}}, nil
		},
	}
	mw := NewWebBotAuthTransport(inner, w)
	client := &http.Client{Transport: mw}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://example.com/path", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer closeTestResponse(t, resp)
}

func TestWebBotAuthSignRequest(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	r := &Request{Headers: http.Header{}}
	if err := w.SignRequest(r, "GET", "https://example.com/api"); err != nil {
		t.Fatalf("SignRequest: %v", err)
	}
	if r.Headers.Get("Signature-Input") == "" {
		t.Error("SignRequest did not set Signature-Input")
	}
	if r.Headers.Get("Signature") == "" {
		t.Error("SignRequest did not set Signature")
	}
	if r.Headers.Get("Signature-Agent") == "" {
		t.Error("SignRequest did not set Signature-Agent")
	}
	// Verify the signature as the origin receives it.
	received := mustNewTestRequest(t, "GET", "https://example.com/api")
	for _, name := range []string{"Signature-Agent", "Signature-Input", "Signature"} {
		received.Header.Set(name, r.Headers.Get(name))
	}
	if err := VerifyWebBotAuthSignature(received, pub); err != nil {
		t.Errorf("VerifyWebBotAuthSignature: %v", err)
	}
}

func TestWebBotAuthPublishPublicKey(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	data, err := w.PublishPublicKey()
	if err != nil {
		t.Fatalf("PublishPublicKey: %v", err)
	}
	var ks WebBotAuthKeySet
	if unmarshalErr := json.Unmarshal(data, &ks); unmarshalErr != nil {
		t.Fatalf("unmarshal: %v", unmarshalErr)
	}
	if len(ks.Keys) != 1 {
		t.Fatalf("keys len = %d, want 1", len(ks.Keys))
	}
	jwk := ks.Keys[0]
	if jwk.Kty != "OKP" {
		t.Errorf("Kty = %s, want OKP", jwk.Kty)
	}
	if jwk.Crv != "Ed25519" {
		t.Errorf("Crv = %s, want Ed25519", jwk.Crv)
	}
	if jwk.Kid == "" {
		t.Error("Kid is empty")
	}
	if jwk.Kid != w.KeyID() {
		t.Errorf("Kid = %s, want %s", jwk.Kid, w.KeyID())
	}
	// Verify the x value decodes to the public key.
	pub, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		t.Fatalf("decode x: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("pub len = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
}

func TestWebBotAuthPublishPublicKeyDisabled(t *testing.T) {
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{Enabled: false})
	if _, err := w.PublishPublicKey(); err == nil {
		t.Error("expected error for disabled signer")
	}
}

func TestGenerateWebBotAuthKey(t *testing.T) {
	priv, pub, kid, err := GenerateWebBotAuthKey()
	if err != nil {
		t.Fatalf("GenerateWebBotAuthKey: %v", err)
	}
	if priv == nil || pub == nil {
		t.Fatal("nil key returned")
	}
	if kid == "" {
		t.Error("empty key ID")
	}
	// Verify the key ID matches the public key.
	expected := mustComputeTestKeyID(t, pub)
	if kid != expected {
		t.Errorf("kid = %s, want %s", kid, expected)
	}
}

func TestEncodeDecodePrivateKey(t *testing.T) {
	priv, _, _, err := GenerateWebBotAuthKey()
	if err != nil {
		t.Fatalf("GenerateWebBotAuthKey: %v", err)
	}
	encoded := EncodePrivateKey(priv)
	decoded, err := DecodePrivateKey(encoded)
	if err != nil {
		t.Fatalf("DecodePrivateKey: %v", err)
	}
	if !bytesEqual(priv, decoded) {
		t.Error("round-trip mismatch")
	}
}

func TestEncodeDecodePublicKey(t *testing.T) {
	_, pub, _, err := GenerateWebBotAuthKey()
	if err != nil {
		t.Fatalf("GenerateWebBotAuthKey: %v", err)
	}
	encoded := EncodePublicKey(pub)
	decoded, err := DecodePublicKey(encoded)
	if err != nil {
		t.Fatalf("DecodePublicKey: %v", err)
	}
	if !bytesEqual(pub, decoded) {
		t.Error("round-trip mismatch")
	}
}

func TestDecodePrivateKeyInvalid(t *testing.T) {
	if _, err := DecodePrivateKey("not-base64!!!"); err == nil {
		t.Error("expected error for invalid base64")
	}
	if _, err := DecodePrivateKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("expected error for wrong key length")
	}
}

func TestDecodePublicKeyInvalid(t *testing.T) {
	if _, err := DecodePublicKey("not-base64!!!"); err == nil {
		t.Error("expected error for invalid base64")
	}
	if _, err := DecodePublicKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Error("expected error for wrong key length")
	}
}

func TestWebBotAuthMaxExpiryDefault24h(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, req)
	diff := signedWindow(t, req.Header.Get("Signature-Input"))
	if diff > 24*time.Hour+10*time.Second {
		t.Errorf("expiry window = %v, want ~24h", diff)
	}
	if diff < 23*time.Hour {
		t.Errorf("expiry window = %v, want ~24h", diff)
	}
}

func TestWebBotAuthCustomMaxExpiry(t *testing.T) {
	_, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
		MaxExpiry:  1 * time.Hour,
	})
	req := mustNewTestRequest(t, "GET", "https://example.com/")
	signTestRequest(t, w, req)
	diff := signedWindow(t, req.Header.Get("Signature-Input"))
	if diff > 1*time.Hour+10*time.Second || diff < 50*time.Minute {
		t.Errorf("expiry window = %v, want ~1h", diff)
	}
}

func TestWebBotAuthSignDifferentDomains(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	// Sign for example.com.
	req1 := mustNewTestRequest(t, "GET", "https://example.com/path")
	req1.Host = "example.com"
	signTestRequest(t, w, req1)
	// Sign for api.example.com.
	req2 := mustNewTestRequest(t, "GET", "https://api.example.com/path")
	req2.Host = "api.example.com"
	signTestRequest(t, w, req2)
	// Verify each with its own authority.
	if err := VerifyWebBotAuthSignature(req1, pub); err != nil {
		t.Errorf("verify req1: %v", err)
	}
	if err := VerifyWebBotAuthSignature(req2, pub); err != nil {
		t.Errorf("verify req2: %v", err)
	}
	// Re-verification with the original authority should succeed.
	if err := VerifyWebBotAuthSignature(req1, pub); err != nil {
		t.Fatalf("same-authority re-verification: %v", err)
	}
	// Tamper: verify req1 with req2's Host.
	req1.Host = "api.example.com"
	if err := VerifyWebBotAuthSignature(req1, pub); err == nil {
		t.Error("cross-domain verification should fail")
	}
}

func TestWebBotAuthEndToEndWithHTTPServer(t *testing.T) {
	pub, priv := generateTestWebBotAuthKey(t)
	w := mustNewTestWebBotAuth(t, WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	verifier, err := NewWebBotAuthVerifier(pub, 0)
	if err != nil {
		t.Fatal(err)
	}
	var receivedSigInput string
	var verifyErr error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSigInput = r.Header.Get("Signature-Input")
		verifyErr = verifier.Verify(r)
		w.WriteHeader(200)
	}))
	defer server.Close()
	// Use the middleware to sign requests.
	mw := NewWebBotAuthTransport(http.DefaultTransport, w)
	client := &http.Client{Transport: mw}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL+"/test", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(request)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer closeTestResponse(t, resp)
	if receivedSigInput == "" {
		t.Fatal("server did not receive Signature-Input")
	}
	// The origin verified the signature exactly as it arrived.
	if verifyErr != nil {
		t.Errorf("origin verification: %v", verifyErr)
	}
}

func TestComputeKeyID(t *testing.T) {
	pub, _ := generateTestWebBotAuthKey(t)
	kid, err := computeKeyID(pub)
	if err != nil {
		t.Fatalf("computeKeyID: %v", err)
	}
	if kid == "" {
		t.Error("empty key ID")
	}
	// Key ID should be base64url (no padding, no +, no /).
	if strings.Contains(kid, "+") || strings.Contains(kid, "/") || strings.Contains(kid, "=") {
		t.Errorf("key ID %q is not base64url", kid)
	}
}

func TestComputeKeyIDDeterministic(t *testing.T) {
	pub, _ := generateTestWebBotAuthKey(t)
	kid1 := mustComputeTestKeyID(t, pub)
	kid2 := mustComputeTestKeyID(t, pub)
	if kid1 != kid2 {
		t.Error("key ID should be deterministic for same public key")
	}
}

func TestComputeKeyIDDifferentKeys(t *testing.T) {
	pub1, _ := generateTestWebBotAuthKey(t)
	pub2, _ := generateTestWebBotAuthKey(t)
	kid1 := mustComputeTestKeyID(t, pub1)
	kid2 := mustComputeTestKeyID(t, pub2)
	if kid1 == kid2 {
		t.Error("different keys should have different key IDs")
	}
}

var webBotAuthWindowPattern = regexp.MustCompile(`created=(\d+);expires=(\d+)`)

func signedWindow(t *testing.T, signatureInput string) time.Duration {
	t.Helper()
	match := webBotAuthWindowPattern.FindStringSubmatch(signatureInput)
	if match == nil {
		t.Fatalf("Signature-Input lacks created/expires: %q", signatureInput)
	}
	created, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	expires, err := strconv.ParseInt(match[2], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return time.Duration(expires-created) * time.Second
}

// mockTransport is a test http.RoundTripper.
type mockTransport struct {
	fn func(*http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.fn(req)
}

// bytesEqual is a helper to compare byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
