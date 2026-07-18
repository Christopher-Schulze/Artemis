package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/idna"
)

type TargetKind string

const (
	TargetNavigation  TargetKind = "navigation"
	TargetRedirect    TargetKind = "redirect"
	TargetSubframe    TargetKind = "subframe"
	TargetWorker      TargetKind = "worker"
	TargetSubresource TargetKind = "subresource"
	TargetWebSocket   TargetKind = "websocket"
	TargetDownload    TargetKind = "download"
	TargetProxy       TargetKind = "proxy"
	TargetSocket      TargetKind = "socket"
)

type DecisionAction string

const (
	DecisionAllow DecisionAction = "allow"
	DecisionDeny  DecisionAction = "deny"
)

var (
	ErrPolicyDenied  = errors.New("network policy denied")
	ErrDNSFailure    = errors.New("network policy DNS failure")
	ErrDecisionAudit = errors.New("network policy decision audit failed")
)

type Decision struct {
	Action    DecisionAction `json:"action"`
	Kind      TargetKind     `json:"kind"`
	Scheme    string         `json:"scheme"`
	Host      string         `json:"host"`
	Port      int            `json:"port"`
	Reason    string         `json:"reason"`
	SessionID string         `json:"session_id,omitempty"`
}

type DecisionSink func(Decision) error

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type PolicyConfig struct {
	AllowedSchemes       []string
	AllowedDomains       []string
	AllowedPorts         []int
	AllowedMethods       []string
	AllowedContentTypes  []string
	AllowedDownloadTypes []string
	AllowPrivateNetworks bool
	MaxRequestBodyBytes  int64
	MaxResponseBodyBytes int64
	MaxDownloadBytes     int64
	MaxRedirects         int
	DialTimeout          time.Duration
}

func DefaultPolicyConfig() PolicyConfig {
	return PolicyConfig{
		AllowedSchemes:       []string{"http", "https", "ws", "wss"},
		AllowedPorts:         []int{80, 443},
		AllowedMethods:       []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedContentTypes:  []string{"application/json", "application/x-www-form-urlencoded", "multipart/form-data", "text/plain"},
		AllowedDownloadTypes: []string{"*/*"},
		MaxRequestBodyBytes:  16 << 20,
		MaxResponseBodyBytes: 50 << 20,
		MaxDownloadBytes:     50 << 20,
		MaxRedirects:         10,
		DialTimeout:          10 * time.Second,
	}
}

type Policy struct {
	config   PolicyConfig
	resolver Resolver
	dialer   net.Dialer
	sink     DecisionSink
}

func NewPolicy(config PolicyConfig, resolver Resolver, sink DecisionSink) (*Policy, error) {
	config = normalizePolicyConfig(config)
	if err := validatePolicyConfig(config); err != nil {
		return nil, err
	}
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Policy{config: config, resolver: resolver, dialer: net.Dialer{Timeout: config.DialTimeout}, sink: sink}, nil
}

func normalizePolicyConfig(config PolicyConfig) PolicyConfig {
	defaults := DefaultPolicyConfig()
	if len(config.AllowedSchemes) == 0 {
		config.AllowedSchemes = defaults.AllowedSchemes
	}
	if len(config.AllowedPorts) == 0 {
		config.AllowedPorts = defaults.AllowedPorts
	}
	if len(config.AllowedMethods) == 0 {
		config.AllowedMethods = defaults.AllowedMethods
	}
	if len(config.AllowedContentTypes) == 0 {
		config.AllowedContentTypes = defaults.AllowedContentTypes
	}
	if len(config.AllowedDownloadTypes) == 0 {
		config.AllowedDownloadTypes = defaults.AllowedDownloadTypes
	}
	if config.MaxRequestBodyBytes <= 0 {
		config.MaxRequestBodyBytes = defaults.MaxRequestBodyBytes
	}
	if config.MaxResponseBodyBytes <= 0 {
		config.MaxResponseBodyBytes = defaults.MaxResponseBodyBytes
	}
	if config.MaxDownloadBytes <= 0 {
		config.MaxDownloadBytes = defaults.MaxDownloadBytes
	}
	if config.MaxRedirects <= 0 {
		config.MaxRedirects = defaults.MaxRedirects
	}
	if config.DialTimeout <= 0 {
		config.DialTimeout = defaults.DialTimeout
	}
	config.AllowedSchemes = normalizedStrings(config.AllowedSchemes, strings.ToLower)
	config.AllowedMethods = normalizedStrings(config.AllowedMethods, strings.ToUpper)
	config.AllowedContentTypes = normalizedStrings(config.AllowedContentTypes, strings.ToLower)
	config.AllowedDownloadTypes = normalizedStrings(config.AllowedDownloadTypes, strings.ToLower)
	config.AllowedDomains = normalizedStrings(config.AllowedDomains, strings.ToLower)
	config.AllowedPorts = append([]int(nil), config.AllowedPorts...)
	sort.Ints(config.AllowedPorts)
	return config
}

func validatePolicyConfig(config PolicyConfig) error {
	for _, port := range config.AllowedPorts {
		if port < 1 || port > 65535 {
			return fmt.Errorf("network policy: invalid port %d", port)
		}
	}
	for _, pattern := range config.AllowedDomains {
		if pattern == "*" || pattern == "*." || strings.ContainsAny(pattern, "/:@") {
			return fmt.Errorf("network policy: invalid domain pattern %q", pattern)
		}
	}
	for _, pattern := range config.AllowedDownloadTypes {
		if !validMediaTypePattern(pattern) {
			return fmt.Errorf("network policy: invalid download content type %q", pattern)
		}
	}
	return nil
}

func normalizedStrings(values []string, transform func(string) string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = transform(strings.TrimSpace(value))
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (p *Policy) Config() PolicyConfig {
	config := p.config
	config.AllowedSchemes = append([]string(nil), config.AllowedSchemes...)
	config.AllowedDomains = append([]string(nil), config.AllowedDomains...)
	config.AllowedPorts = append([]int(nil), config.AllowedPorts...)
	config.AllowedMethods = append([]string(nil), config.AllowedMethods...)
	config.AllowedContentTypes = append([]string(nil), config.AllowedContentTypes...)
	config.AllowedDownloadTypes = append([]string(nil), config.AllowedDownloadTypes...)
	return config
}

// ValidateDownload enforces the download-specific content and size policy.
// Content type must be a normalized sniffed media type, not a server claim.
func (p *Policy) ValidateDownload(contentType string, size int64, sessionID string) error {
	if err := p.ValidateDownloadSize(size, sessionID); err != nil {
		return err
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if mediaType == "" || !mediaTypeAllowed(p.config.AllowedDownloadTypes, mediaType) {
		return p.deny(TargetDownload, nil, sessionID, "download_content_type_not_allowed")
	}
	return nil
}

// ValidateDownloadSize enforces limits before a browser download has a MIME.
func (p *Policy) ValidateDownloadSize(size int64, sessionID string) error {
	if p == nil {
		return fmt.Errorf("%w: policy required", ErrPolicyDenied)
	}
	if size <= 0 {
		return p.deny(TargetDownload, nil, sessionID, "invalid_download_size")
	}
	if size > p.config.MaxDownloadBytes {
		return p.deny(TargetDownload, nil, sessionID, "download_too_large")
	}
	return nil
}

func (p *Policy) ValidateRequest(ctx context.Context, rawURL, method, contentType string, contentLength int64, kind TargetKind, sessionID string) error {
	if p == nil {
		return fmt.Errorf("%w: policy required", ErrPolicyDenied)
	}
	if !containsString(p.config.AllowedMethods, strings.ToUpper(strings.TrimSpace(method))) {
		return p.deny(kind, nil, sessionID, "method_not_allowed")
	}
	if contentLength < 0 {
		return p.deny(kind, nil, sessionID, "request_body_length_unknown")
	}
	if contentLength > p.config.MaxRequestBodyBytes {
		return p.deny(kind, nil, sessionID, "request_body_too_large")
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if contentLength > 0 && mediaType != "" && !containsString(p.config.AllowedContentTypes, mediaType) {
		return p.deny(kind, nil, sessionID, "content_type_not_allowed")
	}
	_, err := p.ResolveURL(ctx, rawURL, kind, sessionID)
	return err
}

func (p *Policy) ResolveURL(ctx context.Context, rawURL string, kind TargetKind, sessionID string) ([]netip.Addr, error) {
	parsed, err := parsePolicyURL(rawURL)
	if err != nil {
		return nil, p.deny(kind, parsed, sessionID, "invalid_url")
	}
	if !containsString(p.config.AllowedSchemes, parsed.Scheme) {
		return nil, p.deny(kind, parsed, sessionID, "scheme_not_allowed")
	}
	port, err := policyPort(parsed)
	if err != nil || !containsInt(p.config.AllowedPorts, port) {
		return nil, p.deny(kind, parsed, sessionID, "port_not_allowed")
	}
	host, err := normalizePolicyHost(parsed.Hostname())
	if err != nil || blockedHostname(host) || !domainAllowed(host, p.config.AllowedDomains) {
		return nil, p.deny(kind, parsed, sessionID, "host_not_allowed")
	}
	addresses, err := p.resolveHost(ctx, host)
	if err != nil {
		return nil, p.deny(kind, parsed, sessionID, "dns_failed")
	}
	for _, address := range addresses {
		if alwaysBlockedAddress(address) {
			return nil, p.deny(kind, parsed, sessionID, "non_destination_address")
		}
		if !p.config.AllowPrivateNetworks && blockedAddress(address) {
			return nil, p.deny(kind, parsed, sessionID, "non_public_address")
		}
	}
	if err := p.emit(Decision{Action: DecisionAllow, Kind: kind, Scheme: parsed.Scheme, Host: host, Port: port, Reason: "policy_match", SessionID: sessionID}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPolicyDenied, err)
	}
	return addresses, nil
}

func (p *Policy) DialContext(ctx context.Context, networkName, address string) (net.Conn, error) {
	return p.dialContext(ctx, networkName, address, "")
}

// DialContextFor binds redacted socket-decision evidence to one session.
func (p *Policy) DialContextFor(sessionID string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, networkName, address string) (net.Conn, error) {
		return p.dialContext(ctx, networkName, address, SessionID(ctx, sessionID))
	}
}

func (p *Policy) dialContext(ctx context.Context, networkName, address, sessionID string) (net.Conn, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, p.dialDeny(networkName, "", 0, sessionID, "invalid_dial_address")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || !containsInt(p.config.AllowedPorts, port) {
		return nil, p.dialDeny(networkName, host, port, sessionID, "port_not_allowed")
	}
	host, err = normalizePolicyHost(host)
	if err != nil || blockedHostname(host) || !domainAllowed(host, p.config.AllowedDomains) {
		return nil, p.dialDeny(networkName, host, port, sessionID, "host_not_allowed")
	}
	addresses, err := p.resolveHost(ctx, host)
	if err != nil {
		return nil, errors.Join(ErrDNSFailure, p.dialDeny(networkName, host, port, sessionID, "dns_failed"))
	}
	var dialErrors []string
	for _, candidate := range addresses {
		if alwaysBlockedAddress(candidate) {
			return nil, p.dialDeny(networkName, host, port, sessionID, "non_destination_address")
		}
		if !p.config.AllowPrivateNetworks && blockedAddress(candidate) {
			return nil, p.dialDeny(networkName, host, port, sessionID, "non_public_address")
		}
	}
	if err := p.emit(Decision{Action: DecisionAllow, Kind: TargetSocket, Scheme: strings.ToLower(networkName), Host: host, Port: port, Reason: "policy_match", SessionID: sessionID}); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrPolicyDenied, err)
	}
	for _, candidate := range addresses {
		connection, dialErr := p.dialer.DialContext(ctx, networkName, net.JoinHostPort(candidate.String(), portText))
		if dialErr == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, dialErr.Error())
	}
	return nil, fmt.Errorf("network policy dial failed: %s", strings.Join(dialErrors, "; "))
}

func (p *Policy) resolveHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, ok := parsePolicyAddress(host); ok {
		return []netip.Addr{address}, nil
	}
	addresses, err := p.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrDNSFailure
	}
	set := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		set[address.Unmap()] = struct{}{}
	}
	result := make([]netip.Addr, 0, len(set))
	for address := range set {
		result = append(result, address)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Compare(result[j]) < 0 })
	return result, nil
}

func parsePolicyURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil {
		return parsed, errors.New("invalid network URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	return parsed, nil
}

func normalizePolicyHost(host string) (string, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || strings.Contains(host, "%") {
		return "", errors.New("invalid host")
	}
	if _, ok := parsePolicyAddress(host); ok {
		return host, nil
	}
	ascii, err := idna.Lookup.ToASCII(host)
	if err != nil || ascii == "" {
		return "", errors.New("invalid IDNA host")
	}
	return strings.ToLower(ascii), nil
}

func parsePolicyAddress(host string) (netip.Addr, bool) {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Unmap(), true
	}
	value, ok := parseLegacyIPv4(host)
	if !ok {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)}), true
}

func parseLegacyIPv4(host string) (uint32, bool) {
	parts := strings.Split(host, ".")
	if len(parts) < 1 || len(parts) > 4 {
		return 0, false
	}
	values := make([]uint64, len(parts))
	for i, part := range parts {
		base := 10
		if strings.HasPrefix(part, "0x") || strings.HasPrefix(part, "0X") {
			base, part = 16, part[2:]
		} else if len(part) > 1 && part[0] == '0' {
			base, part = 8, part[1:]
		}
		if part == "" {
			part = "0"
		}
		value, err := strconv.ParseUint(part, base, 32)
		if err != nil {
			return 0, false
		}
		values[i] = value
	}
	var result uint64
	switch len(values) {
	case 1:
		result = values[0]
	case 2:
		if values[0] > 0xff || values[1] > 0xffffff {
			return 0, false
		}
		result = values[0]<<24 | values[1]
	case 3:
		if values[0] > 0xff || values[1] > 0xff || values[2] > 0xffff {
			return 0, false
		}
		result = values[0]<<24 | values[1]<<16 | values[2]
	case 4:
		for _, value := range values {
			if value > 0xff {
				return 0, false
			}
		}
		result = values[0]<<24 | values[1]<<16 | values[2]<<8 | values[3]
	}
	if result > 0xffffffff {
		return 0, false
	}
	return uint32(result), true
}

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"), netip.MustParsePrefix("ff00::/8"),
}

func alwaysBlockedAddress(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsValid() || address.IsUnspecified() || address.IsMulticast()
}

func blockedAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() {
		return true
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func blockedHostname(host string) bool {
	return host == "localhost" || host == "localhost.localdomain" || host == "metadata.google.internal" || host == "metadata.goog" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal")
}

func domainAllowed(host string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*.")
			if host != suffix && strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == pattern {
			return true
		}
	}
	return false
}

func policyPort(parsed *url.URL) (int, error) {
	if parsed.Port() != "" {
		return strconv.Atoi(parsed.Port())
	}
	switch parsed.Scheme {
	case "http", "ws":
		return 80, nil
	case "https", "wss":
		return 443, nil
	default:
		return 0, errors.New("unsupported scheme")
	}
}

func (p *Policy) deny(kind TargetKind, parsed *url.URL, sessionID, reason string) error {
	decision := Decision{Action: DecisionDeny, Kind: kind, Reason: reason, SessionID: sessionID}
	if parsed != nil {
		decision.Scheme = parsed.Scheme
		decision.Host = safeDecisionHost(parsed.Hostname())
		decision.Port, _ = policyPort(parsed)
	}
	auditErr := p.emit(decision)
	denied := fmt.Errorf("%w: %s", ErrPolicyDenied, reason)
	if auditErr != nil {
		return errors.Join(denied, auditErr)
	}
	return denied
}

func (p *Policy) dialDeny(networkName, host string, port int, sessionID, reason string) error {
	decision := Decision{
		Action: DecisionDeny, Kind: TargetSocket, Scheme: strings.ToLower(networkName),
		Host: safeDecisionHost(host), Port: port, Reason: reason, SessionID: sessionID,
	}
	denied := fmt.Errorf("%w: %s", ErrPolicyDenied, reason)
	if err := p.emit(decision); err != nil {
		return errors.Join(denied, err)
	}
	return denied
}

func safeDecisionHost(host string) string {
	normalized, err := normalizePolicyHost(host)
	if err != nil {
		return ""
	}
	return normalized
}

func (p *Policy) emit(decision Decision) error {
	if p.sink != nil {
		if err := p.sink(decision); err != nil {
			return fmt.Errorf("%w: %w", ErrDecisionAudit, err)
		}
	}
	return nil
}

func containsString(values []string, value string) bool {
	index := sort.SearchStrings(values, value)
	return index < len(values) && values[index] == value
}

func containsInt(values []int, value int) bool {
	index := sort.SearchInts(values, value)
	return index < len(values) && values[index] == value
}

func validMediaTypePattern(pattern string) bool {
	parts := strings.Split(pattern, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	if parts[0] == "*" {
		return parts[1] == "*"
	}
	return validMediaToken(parts[0]) && (parts[1] == "*" || validMediaToken(parts[1]))
}

func validMediaToken(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || strings.ContainsRune("!#$%&'+-.^_`|~", char) {
			continue
		}
		return false
	}
	return true
}

func mediaTypeAllowed(patterns []string, mediaType string) bool {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, pattern := range patterns {
		if pattern == "*/*" || pattern == mediaType || strings.HasSuffix(pattern, "/*") && strings.TrimSuffix(pattern, "/*") == parts[0] {
			return true
		}
	}
	return false
}
