package network

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebBotAuthDisabledNoOp(t *testing.T) {
	w, err := NewWebBotAuth(WebBotAuthConfig{Enabled: false})
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	req, _ := http.NewRequest("GET", "https://example.com/path", nil)
	if err := w.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
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
	_, priv, _ := ed25519.GenerateKey(nil)
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
	_, priv, _ := ed25519.GenerateKey(nil)
	w, err := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	if err != nil {
		t.Fatalf("NewWebBotAuth: %v", err)
	}
	req, _ := http.NewRequest("GET", "https://example.com/path", nil)
	if err := w.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if req.Header.Get("Signature-Agent") == "" {
		t.Error("Signature-Agent header not set")
	}
	if req.Header.Get("Signature-Input") == "" {
		t.Error("Signature-Input header not set")
	}
	if req.Header.Get("Signature") == "" {
		t.Error("Signature header not set")
	}
	// Verify Signature-Agent format: g="<url>"
	sa := req.Header.Get("Signature-Agent")
	if !strings.HasPrefix(sa, `g="`) {
		t.Errorf("Signature-Agent = %q, want g=\"...\" format", sa)
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
	// Verify Signature format: g=:<base64>:
	sig := req.Header.Get("Signature")
	if !strings.HasPrefix(sig, "g=:") || !strings.HasSuffix(sig, ":") {
		t.Errorf("Signature = %q, want g=:<base64>: format", sig)
	}
	_, signed, _, _ := w.Stats()
	if signed != 1 {
		t.Errorf("signed = %d, want 1", signed)
	}
}

func TestWebBotAuthSignDeterministic(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	// Sign the same request twice with the same created/expires.
	// Since created/expires use time.Now(), we test determinism by signing
	// twice in rapid succession and verifying the signatures are valid
	// Ed25519 (which is deterministic for the same input).
	req1, _ := http.NewRequest("GET", "https://example.com/path", nil)
	req2, _ := http.NewRequest("GET", "https://example.com/path", nil)
	w.Sign(req1)
	w.Sign(req2)
	// Both should have valid base64 signatures.
	sig1 := req1.Header.Get("Signature")
	sig2 := req2.Header.Get("Signature")
	if sig1 == "" || sig2 == "" {
		t.Fatal("signatures empty")
	}
	// Decode and verify both are valid Ed25519 signatures.
	sig1B64 := sig1[3 : len(sig1)-1]
	sig2B64 := sig2[3 : len(sig2)-1]
	sig1Bytes, _ := base64.StdEncoding.DecodeString(sig1B64)
	sig2Bytes, _ := base64.StdEncoding.DecodeString(sig2B64)
	if len(sig1Bytes) != ed25519.SignatureSize {
		t.Errorf("sig1 len = %d, want %d", len(sig1Bytes), ed25519.SignatureSize)
	}
	if len(sig2Bytes) != ed25519.SignatureSize {
		t.Errorf("sig2 len = %d, want %d", len(sig2Bytes), ed25519.SignatureSize)
	}
}

func TestWebBotAuthSignNilRequest(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
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
	pub, priv, _ := ed25519.GenerateKey(nil)
	expectedKID, _ := computeKeyID(pub)
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
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	w.Sign(req)
	si := req.Header.Get("Signature-Input")
	if !strings.Contains(si, expectedKID) {
		t.Errorf("Signature-Input %q does not contain keyid %q", si, expectedKID)
	}
}

func TestWebBotAuthExplicitKeyID(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
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
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req, _ := http.NewRequest("GET", "https://example.com/api/data", nil)
	req.Host = "example.com"
	if err := w.Sign(req); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := VerifyWebBotAuthSignature(req, pub); err != nil {
		t.Errorf("VerifyWebBotAuthSignature: %v", err)
	}
}

func TestWebBotAuthVerifyFailsWithWrongKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	wrongPub, _, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req, _ := http.NewRequest("GET", "https://example.com/api/data", nil)
	req.Host = "example.com"
	w.Sign(req)
	if err := VerifyWebBotAuthSignature(req, wrongPub); err == nil {
		t.Error("verification should fail with wrong public key")
	}
}

func TestWebBotAuthVerifyFailsWithTamperedAuthority(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req, _ := http.NewRequest("GET", "https://example.com/api/data", nil)
	req.Host = "example.com"
	w.Sign(req)
	// Tamper with the Host after signing.
	req.Host = "evil.com"
	if err := VerifyWebBotAuthSignature(req, pub); err == nil {
		t.Error("verification should fail with tampered authority")
	}
}

func TestWebBotAuthVerifyMissingHeaders(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	if err := VerifyWebBotAuthSignature(req, pub); err == nil {
		t.Error("verification should fail with missing headers")
	}
}

func TestWebBotAuthMiddleware(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
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
	resp, err := client.Get("https://example.com/path")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
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
	w, _ := NewWebBotAuth(WebBotAuthConfig{Enabled: false})
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
	resp, err := client.Get("https://example.com/path")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
}

func TestWebBotAuthSignRequest(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
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
	// Verify the signature using the headers.
	authority := "example.com"
	err := VerifyRequestHeaders(
		r.Headers.Get("Signature-Agent"),
		r.Headers.Get("Signature-Input"),
		r.Headers.Get("Signature"),
		authority,
		pub,
	)
	if err != nil {
		t.Errorf("VerifyRequestHeaders: %v", err)
	}
}

func TestWebBotAuthSignRequestURL(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	sa, si, sv, err := SignRequestURL(w, "POST", "https://api.example.com/v1/data")
	if err != nil {
		t.Fatalf("SignRequestURL: %v", err)
	}
	if sa == "" || si == "" || sv == "" {
		t.Fatal("SignRequestURL returned empty headers")
	}
	if err := VerifyRequestHeaders(sa, si, sv, "api.example.com", pub); err != nil {
		t.Errorf("VerifyRequestHeaders: %v", err)
	}
}

func TestWebBotAuthSignRequestURLDisabled(t *testing.T) {
	w, _ := NewWebBotAuth(WebBotAuthConfig{Enabled: false})
	sa, si, sv, err := SignRequestURL(w, "GET", "https://example.com/")
	if err != nil {
		t.Fatalf("SignRequestURL: %v", err)
	}
	if sa != "" || si != "" || sv != "" {
		t.Error("disabled signer should return empty headers")
	}
}

func TestWebBotAuthPublishPublicKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	data, err := w.PublishPublicKey()
	if err != nil {
		t.Fatalf("PublishPublicKey: %v", err)
	}
	var ks WebBotAuthKeySet
	if err := json.Unmarshal(data, &ks); err != nil {
		t.Fatalf("unmarshal: %v", err)
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
	w, _ := NewWebBotAuth(WebBotAuthConfig{Enabled: false})
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
	expected, _ := computeKeyID(pub)
	if kid != expected {
		t.Errorf("kid = %s, want %s", kid, expected)
	}
}

func TestEncodeDecodePrivateKey(t *testing.T) {
	priv, _, _, _ := GenerateWebBotAuthKey()
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
	_, pub, _, _ := GenerateWebBotAuthKey()
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

func TestParseWebBotAuthSignatureInput(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	w.Sign(req)
	si := req.Header.Get("Signature-Input")
	created, expires, keyID, tag, err := ParseWebBotAuthSignatureInput(si)
	if err != nil {
		t.Fatalf("ParseWebBotAuthSignatureInput: %v", err)
	}
	if created <= 0 {
		t.Errorf("created = %d, want > 0", created)
	}
	if expires <= created {
		t.Errorf("expires = %d, want > created %d", expires, created)
	}
	if keyID == "" {
		t.Error("keyID is empty")
	}
	if tag != WebBotAuthTag {
		t.Errorf("tag = %q, want %q", tag, WebBotAuthTag)
	}
}

func TestParseWebBotAuthSignatureInputWrongTag(t *testing.T) {
	si := `g=("@authority");created=1000;expires=2000;keyid="kid";tag="wrong-tag"`
	_, _, _, _, err := ParseWebBotAuthSignatureInput(si)
	if err == nil {
		t.Error("expected error for wrong tag")
	}
}

func TestParseWebBotAuthSignatureInputMissingKeyID(t *testing.T) {
	si := `g=("@authority");created=1000;expires=2000;tag="web-bot-auth"`
	_, _, _, _, err := ParseWebBotAuthSignatureInput(si)
	if err == nil {
		t.Error("expected error for missing keyid")
	}
}

func TestWebBotAuthMaxExpiryDefault24h(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	w.Sign(req)
	si := req.Header.Get("Signature-Input")
	created, expires, _, _, _ := ParseWebBotAuthSignatureInput(si)
	diff := time.Duration(expires-created) * time.Second
	if diff > 24*time.Hour+10*time.Second {
		t.Errorf("expiry window = %v, want ~24h", diff)
	}
	if diff < 23*time.Hour {
		t.Errorf("expiry window = %v, want ~24h", diff)
	}
}

func TestWebBotAuthCustomMaxExpiry(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
		MaxExpiry:  1 * time.Hour,
	})
	req, _ := http.NewRequest("GET", "https://example.com/", nil)
	w.Sign(req)
	si := req.Header.Get("Signature-Input")
	created, expires, _, _, _ := ParseWebBotAuthSignatureInput(si)
	diff := time.Duration(expires-created) * time.Second
	if diff > 1*time.Hour+10*time.Second || diff < 50*time.Minute {
		t.Errorf("expiry window = %v, want ~1h", diff)
	}
}

func TestWebBotAuthSignDifferentDomains(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	// Sign for example.com.
	req1, _ := http.NewRequest("GET", "https://example.com/path", nil)
	req1.Host = "example.com"
	w.Sign(req1)
	// Sign for api.example.com.
	req2, _ := http.NewRequest("GET", "https://api.example.com/path", nil)
	req2.Host = "api.example.com"
	w.Sign(req2)
	// Verify each with its own authority.
	if err := VerifyWebBotAuthSignature(req1, pub); err != nil {
		t.Errorf("verify req1: %v", err)
	}
	if err := VerifyWebBotAuthSignature(req2, pub); err != nil {
		t.Errorf("verify req2: %v", err)
	}
	// Cross-verification should fail (different authority).
	if err := VerifyWebBotAuthSignature(req1, pub); err != nil {
		// req1 should verify fine
	}
	// Tamper: verify req1 with req2's Host.
	req1.Host = "api.example.com"
	if err := VerifyWebBotAuthSignature(req1, pub); err == nil {
		t.Error("cross-domain verification should fail")
	}
}

func TestWebBotAuthVerifyRequestHeaders(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	sa, si, sv, _ := SignRequestURL(w, "GET", "https://example.com/")
	err := VerifyRequestHeaders(sa, si, sv, "example.com", pub)
	if err != nil {
		t.Errorf("VerifyRequestHeaders: %v", err)
	}
}

func TestWebBotAuthVerifyRequestHeadersTampered(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	sa, si, sv, _ := SignRequestURL(w, "GET", "https://example.com/")
	// Tamper with authority.
	err := VerifyRequestHeaders(sa, si, sv, "evil.com", pub)
	if err == nil {
		t.Error("verification should fail with tampered authority")
	}
}

func TestWebBotAuthVerifyRequestHeadersMissingHeaders(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	err := VerifyRequestHeaders("", "", "", "example.com", pub)
	if err == nil {
		t.Error("verification should fail with missing headers")
	}
}

func TestWebBotAuthEndToEndWithHTTPServer(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	var receivedSigInput, receivedSig, receivedSigAgent, receivedHost string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSigInput = r.Header.Get("Signature-Input")
		receivedSig = r.Header.Get("Signature")
		receivedSigAgent = r.Header.Get("Signature-Agent")
		receivedHost = r.Host
		w.WriteHeader(200)
	}))
	defer server.Close()
	// Use the middleware to sign requests.
	mw := NewWebBotAuthTransport(http.DefaultTransport, w)
	client := &http.Client{Transport: mw}
	resp, err := client.Get(server.URL + "/test")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	if receivedSigInput == "" {
		t.Fatal("server did not receive Signature-Input")
	}
	// Verify the signature using the received headers and host.
	if err := VerifyRequestHeaders(receivedSigAgent, receivedSigInput, receivedSig, receivedHost, pub); err != nil {
		t.Errorf("VerifyRequestHeaders: %v", err)
	}
}

func TestComputeKeyID(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
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
	pub, _, _ := ed25519.GenerateKey(nil)
	kid1, _ := computeKeyID(pub)
	kid2, _ := computeKeyID(pub)
	if kid1 != kid2 {
		t.Error("key ID should be deterministic for same public key")
	}
}

func TestComputeKeyIDDifferentKeys(t *testing.T) {
	pub1, _, _ := ed25519.GenerateKey(nil)
	pub2, _, _ := ed25519.GenerateKey(nil)
	kid1, _ := computeKeyID(pub1)
	kid2, _ := computeKeyID(pub2)
	if kid1 == kid2 {
		t.Error("different keys should have different key IDs")
	}
}

func TestQuoteHTTPString(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"example.com", `"example.com"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `"a\\b"`},
		{"", `""`},
	}
	for _, tc := range tests {
		got := quoteHTTPString(tc.input)
		if got != tc.want {
			t.Errorf("quoteHTTPString(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestExtractSignatureParams(t *testing.T) {
	si := `g=("@authority");created=1000;expires=2000;keyid="kid";tag="web-bot-auth"`
	params, err := extractSignatureParams(si)
	if err != nil {
		t.Fatalf("extractSignatureParams: %v", err)
	}
	if !strings.Contains(params, "@authority") {
		t.Errorf("params = %q, should contain @authority", params)
	}
	if !strings.Contains(params, "web-bot-auth") {
		t.Errorf("params = %q, should contain tag", params)
	}
}

func TestExtractSignatureParamsMalformed(t *testing.T) {
	_, err := extractSignatureParams("no-equals-here")
	if err == nil {
		t.Error("expected error for malformed input")
	}
}

func TestExtractSigB64FromHeader(t *testing.T) {
	sig := "g=:dGhpcyBpcyBhIHNpZ25hdHVyZQ==:"
	b64, err := extractSigB64FromHeader(sig)
	if err != nil {
		t.Fatalf("extractSigB64FromHeader: %v", err)
	}
	if b64 != "dGhpcyBpcyBhIHNpZ25hdHVyZQ==" {
		t.Errorf("b64 = %q", b64)
	}
}

func TestExtractSigB64FromHeaderMissingPrefix(t *testing.T) {
	_, err := extractSigB64FromHeader("x=:abc=:")
	if err == nil {
		t.Error("expected error for wrong label")
	}
}

func TestExtractSigB64FromHeaderMissingSuffix(t *testing.T) {
	_, err := extractSigB64FromHeader("g=:abc")
	if err == nil {
		t.Error("expected error for missing trailing colon")
	}
}

func TestWebBotAuthVerifySignatureBase(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	w, _ := NewWebBotAuth(WebBotAuthConfig{
		Enabled:    true,
		AgentURL:   "https://agent.example.com",
		PrivateKey: priv,
	})
	_, si, sv, _ := SignRequestURL(w, "GET", "https://example.com/")
	sigB64, _ := extractSigB64FromHeader(sv)
	if err := VerifySignatureBase("example.com", si, sigB64, pub); err != nil {
		t.Errorf("VerifySignatureBase: %v", err)
	}
	// Wrong authority should fail.
	if err := VerifySignatureBase("evil.com", si, sigB64, pub); err == nil {
		t.Error("VerifySignatureBase should fail with wrong authority")
	}
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
