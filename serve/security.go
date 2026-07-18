package serve

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	// ClientIDHeader carries the server-issued, bearer-bound client capability.
	// Reusing it lets a reconnecting authenticated client retain session access.
	ClientIDHeader = "X-Artemis-Client-ID"

	defaultConnectionRequestsPerSecond = 30
	defaultConnectionBurst             = 10
	defaultClientRequestsPerMinute     = 120
	defaultClientBurst                 = 10
	defaultClientBucketTTL             = 10 * time.Minute
)

var defaultOriginPatterns = []string{
	"http://localhost:*",
	"https://localhost:*",
	"http://127.0.0.1:*",
	"https://127.0.0.1:*",
	"app://omnimus-mc",
	"wails://localhost",
}

// DefaultOriginPatterns returns the browser origins accepted by a default
// loopback-only server. Callers receive a copy.
func DefaultOriginPatterns() []string {
	return append([]string(nil), defaultOriginPatterns...)
}

// ValidateOriginPatterns rejects universal patterns that would turn an origin
// allowlist into an origin-verification bypass.
func ValidateOriginPatterns(patterns []string) error {
	if len(patterns) == 0 {
		return fmt.Errorf("at least one origin pattern is required")
	}
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return fmt.Errorf("origin patterns cannot be empty")
		}
		hostPattern := pattern
		if _, value, ok := strings.Cut(pattern, "://"); ok {
			hostPattern = value
		}
		hostPattern = strings.TrimSuffix(hostPattern, ":*")
		if hostPattern == "*" {
			return fmt.Errorf("universal origin pattern %q is forbidden", pattern)
		}
	}
	return nil
}

type clientIdentity struct {
	id             string
	ownerRef       string
	rateKey        string
	authGeneration uint64
}

func defaultRateLimit() RateLimit {
	return RateLimit{
		RequestsPerSecond:       defaultConnectionRequestsPerSecond,
		Burst:                   defaultConnectionBurst,
		ClientRequestsPerMinute: defaultClientRequestsPerMinute,
		ClientBurst:             defaultClientBurst,
		ClientBucketTTL:         defaultClientBucketTTL,
	}
}

func normalizeRateLimit(config RateLimit) RateLimit {
	defaults := defaultRateLimit()
	if config.RequestsPerSecond <= 0 {
		config.RequestsPerSecond = defaults.RequestsPerSecond
	}
	if config.Burst <= 0 {
		config.Burst = defaults.Burst
	}
	if config.ClientRequestsPerMinute <= 0 {
		config.ClientRequestsPerMinute = defaults.ClientRequestsPerMinute
	}
	if config.ClientBurst <= 0 {
		config.ClientBurst = defaults.ClientBurst
	}
	if config.ClientBucketTTL <= 0 {
		config.ClientBucketTTL = defaults.ClientBucketTTL
	}
	return config
}

func authenticateBearer(header, token string) bool {
	if token == "" || !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	want := sha256.Sum256([]byte(token))
	got := sha256.Sum256([]byte(strings.TrimPrefix(header, "Bearer ")))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

func issueClientID(signingKey []byte) (string, error) {
	randomID := make([]byte, 16)
	if _, err := rand.Read(randomID); err != nil {
		return "", fmt.Errorf("generate client id: %w", err)
	}
	id := hex.EncodeToString(randomID)
	return id + "." + signClientID(id, signingKey), nil
}

func validateClientID(value string, signingKey []byte) bool {
	id, signature, ok := strings.Cut(value, ".")
	if !ok || len(id) != 32 || len(signature) != 64 {
		return false
	}
	if _, err := hex.DecodeString(id); err != nil {
		return false
	}
	want, err := hex.DecodeString(signClientID(id, signingKey))
	if err != nil {
		return false
	}
	got, err := hex.DecodeString(signature)
	return err == nil && hmac.Equal(got, want)
}

func signClientID(id string, signingKey []byte) string {
	mac := hmac.New(sha256.New, signingKey)
	if _, err := mac.Write([]byte(id)); err != nil {
		return ""
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func clientOwnerRef(clientID string) string {
	id, _, _ := strings.Cut(clientID, ".")
	return "serve:" + id
}

func normalizeClientAddress(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "unknown"
	}
	if address, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return address.Addr().Unmap().String()
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		if address, parseErr := netip.ParseAddr(strings.Trim(host, "[]")); parseErr == nil {
			return address.Unmap().String()
		}
		return strings.ToLower(host)
	}
	if address, err := netip.ParseAddr(strings.Trim(remoteAddr, "[]")); err == nil {
		return address.Unmap().String()
	}
	return strings.ToLower(remoteAddr)
}

func validateLoopbackAddress(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil || !address.IsLoopback() {
		return fmt.Errorf("listen address must use a numeric loopback host")
	}
	return nil
}

func validateRequestHost(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
		port = ""
	}
	if !isLoopbackHost(host) {
		return false
	}
	localAddr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || localAddr == nil || port == "" {
		return true
	}
	_, localPort, err := net.SplitHostPort(localAddr.String())
	return err == nil && port == localPort
}

func hasQueryCredential(r *http.Request) bool {
	for key := range r.URL.Query() {
		switch strings.ToLower(key) {
		case "token", "auth_token", "access_token", "session_token", "sessiontoken", "csrf_token", "csrftoken":
			return true
		}
	}
	return false
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address, err := netip.ParseAddr(host)
	return err == nil && address.IsLoopback()
}

type tokenBucket struct {
	mu         sync.Mutex
	tokens     float64
	burst      float64
	rate       float64
	lastRefill time.Time
	lastSeen   time.Time
}

func newTokenBucket(requests int, interval time.Duration, burst int, now time.Time) *tokenBucket {
	return &tokenBucket{
		tokens:     float64(burst),
		burst:      float64(burst),
		rate:       float64(requests) / interval.Seconds(),
		lastRefill: now,
		lastSeen:   now,
	}
}

func (b *tokenBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	elapsed := now.Sub(b.lastRefill)
	if elapsed > 0 {
		b.tokens += elapsed.Seconds() * b.rate
		if b.tokens > b.burst {
			b.tokens = b.burst
		}
		b.lastRefill = now
	}
	b.lastSeen = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *tokenBucket) staleBefore(cutoff time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.lastSeen.Before(cutoff)
}

type clientRateLimiter struct {
	config RateLimit
	now    func() time.Time
	mu     sync.Mutex
	bucket map[string]*tokenBucket
	lastGC time.Time
}

func newClientRateLimiter(config RateLimit) *clientRateLimiter {
	return &clientRateLimiter{config: config, now: time.Now, bucket: make(map[string]*tokenBucket)}
}

func (l *clientRateLimiter) allow(key string) bool {
	now := l.now()
	l.mu.Lock()
	if l.lastGC.IsZero() || now.Sub(l.lastGC) >= time.Minute {
		cutoff := now.Add(-l.config.ClientBucketTTL)
		for existingKey, bucket := range l.bucket {
			if bucket.staleBefore(cutoff) {
				delete(l.bucket, existingKey)
			}
		}
		l.lastGC = now
	}
	bucket := l.bucket[key]
	if bucket == nil {
		bucket = newTokenBucket(l.config.ClientRequestsPerMinute, time.Minute, l.config.ClientBurst, now)
		l.bucket[key] = bucket
	}
	l.mu.Unlock()
	return bucket.allow(now)
}
