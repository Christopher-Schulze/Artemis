package network

import (
	"context"
	"errors"
	"fmt"
	"math"
	"mime"
	"strings"
)

func validateRawPolicyConfig(config PolicyConfig) error {
	if err := validatePolicyLimits(config); err != nil {
		return err
	}
	for _, scheme := range config.AllowedSchemes {
		switch strings.ToLower(strings.TrimSpace(scheme)) {
		case "http", "https", "ws", "wss":
		default:
			return errors.New("network policy: unsupported scheme")
		}
	}
	for _, method := range config.AllowedMethods {
		method = strings.TrimSpace(method)
		if len(method) > 32 || !validMediaToken(strings.ToLower(method)) {
			return errors.New("network policy: invalid method")
		}
	}
	for _, contentType := range config.AllowedContentTypes {
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil || len(params) != 0 || !validMediaTypePattern(mediaType) || strings.Contains(mediaType, "*") {
			return errors.New("network policy: invalid request content type")
		}
	}
	for _, pattern := range config.AllowedDownloadTypes {
		if !validMediaTypePattern(strings.ToLower(strings.TrimSpace(pattern))) {
			return errors.New("network policy: invalid download content type")
		}
	}
	return nil
}

func validatePolicyLimits(config PolicyConfig) error {
	if len(config.AllowedDomains) > 256 || len(config.AllowedSchemes) > 64 || len(config.AllowedMethods) > 64 ||
		len(config.AllowedContentTypes) > 64 || len(config.AllowedDownloadTypes) > 64 || len(config.AllowedPorts) > 65535 {
		return errors.New("network policy: configuration list too large")
	}
	if config.MaxRequestBodyBytes < 0 || config.MaxResponseBodyBytes < 0 || config.MaxDownloadBytes < 0 ||
		config.MaxRequestBodyBytes == math.MaxInt64 || config.MaxResponseBodyBytes == math.MaxInt64 || config.MaxDownloadBytes == math.MaxInt64 ||
		config.MaxRedirects < 0 || config.DialTimeout < 0 {
		return errors.New("network policy: invalid limit")
	}
	return nil
}

func normalizePolicyDomains(domains []string) ([]string, error) {
	result := make([]string, 0, len(domains))
	for _, pattern := range domains {
		pattern = strings.TrimSpace(pattern)
		wildcard := strings.HasPrefix(pattern, "*.")
		host, err := normalizePolicyHost(strings.TrimPrefix(pattern, "*."))
		if err != nil || strings.ContainsAny(pattern, "/:@") || strings.Contains(host, "*") {
			return nil, errors.New("network policy: invalid domain pattern")
		}
		if wildcard {
			if _, numeric := parsePolicyAddress(host); numeric {
				return nil, errors.New("network policy: wildcard IP address")
			}
			host = "*." + host
		}
		result = append(result, host)
	}
	return result, nil
}

func validDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func (p *Policy) validateContext(ctx context.Context) error {
	if p == nil || p.resolver == nil || p.config.DialTimeout <= 0 || len(p.config.AllowedSchemes) == 0 {
		return fmt.Errorf("%w: initialized policy required", ErrPolicyDenied)
	}
	if ctx == nil {
		return fmt.Errorf("%w: context required", ErrPolicyDenied)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrPolicyDenied, err)
	}
	return nil
}

func validTargetKind(kind TargetKind) bool {
	switch kind {
	case TargetNavigation, TargetRedirect, TargetSubframe, TargetWorker, TargetSubresource, TargetWebSocket, TargetDownload, TargetProxy, TargetSocket:
		return true
	default:
		return false
	}
}
