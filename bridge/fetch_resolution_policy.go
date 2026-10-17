package bridge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

// ErrFetchAlreadyResolved rejects a second terminal command for one request.
var ErrFetchAlreadyResolved = errors.New("fetch request already resolved")

type fetchResolver struct {
	page          *Page
	request       FetchRequest
	contentLength int64
	kind          network.TargetKind
	mu            sync.Mutex
	terminal      bool
}

func (p *Page) newFetchResolver(job fetchRequestJob) (*fetchResolver, error) {
	request := fetchRequest(job)
	headers := make(http.Header, len(job.payload.Request.Headers))
	for key, value := range job.payload.Request.Headers {
		text, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%w: invalid_native_header", network.ErrPolicyDenied)
		}
		headers[key] = []string{text}
	}
	validated, err := network.ValidatedHeaders(headers)
	if err != nil {
		return nil, err
	}
	request.Headers = make(map[string]string, len(validated))
	for key := range validated {
		request.Headers[key] = validated.Get(key)
	}
	return &fetchResolver{
		page: p, request: request, kind: p.policyTargetKind(job.sessionID, job.payload),
		contentLength: fetchContentLength(job.payload.Request.Headers, request.PostData, request.HasPostData),
	}, nil
}

func (r *fetchResolver) requestCopy() FetchRequest {
	request := r.request
	request.Headers = make(map[string]string, len(r.request.Headers))
	for key, value := range r.request.Headers {
		request.Headers[key] = value
	}
	return request
}

func (r *fetchResolver) resolved() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminal
}

func (r *fetchResolver) resolve(ctx context.Context, resolution FetchResolution) error {
	if ctx == nil {
		return fmt.Errorf("resolve fetch request: context required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.page.ensureAttached(); err != nil {
		return err
	}
	if !r.page.ownsPolicySession(r.request.SessionID) {
		return fmt.Errorf("%w: unowned_fetch_session", network.ErrPolicyDenied)
	}
	policy, err := r.page.NetworkPolicy()
	if err != nil {
		return err
	}
	if err := r.claimResolution(); err != nil {
		return err
	}
	resolution.Headers = copyFetchHeaders(resolution.Headers)
	resolution.ResponseHeaders = copyFetchHeaders(resolution.ResponseHeaders)
	validated, validationErr := r.validate(ctx, policy, resolution)
	if validationErr != nil {
		return r.rejectResolution(ctx, validationErr)
	}
	return r.page.resolveFetchRequest(ctx, r.request.SessionID, r.request.RequestID, validated)
}

func (r *fetchResolver) rejectResolution(ctx context.Context, denial error) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	callErr := r.page.resolveFetchRequest(cleanupCtx, r.request.SessionID, r.request.RequestID,
		FetchResolution{Kind: FetchResolutionFail, ErrorReason: "BlockedByClient"})
	return errors.Join(denial, callErr)
}

func (r *fetchResolver) claimResolution() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal {
		return ErrFetchAlreadyResolved
	}
	r.terminal = true
	return nil
}

func (r *fetchResolver) validate(ctx context.Context, policy *network.Policy, resolution FetchResolution) (FetchResolution, error) {
	switch resolution.Kind {
	case FetchResolutionContinue:
		return r.validateContinuation(ctx, policy, resolution)
	case FetchResolutionFulfill:
		return validateFulfillment(ctx, policy, r.request.SessionID, r.kind, resolution)
	case FetchResolutionFail, FetchResolutionTimeout:
		return resolution, nil
	default:
		return resolution, fmt.Errorf("%w: invalid_fetch_resolution", network.ErrPolicyDenied)
	}
}

func (r *fetchResolver) validateContinuation(ctx context.Context, policy *network.Policy, resolution FetchResolution) (FetchResolution, error) {
	target, method := r.request.URL, r.request.Method
	length := r.contentLength
	if resolution.URL != "" {
		target = resolution.URL
	}
	if resolution.Method != "" {
		method = strings.ToUpper(strings.TrimSpace(resolution.Method))
		resolution.Method = method
	}
	headers, err := r.continuationHeaders(resolution.Headers)
	if err != nil {
		return resolution, err
	}
	if resolution.PostData != "" {
		limit := policy.Config().MaxRequestBodyBytes
		maximumDecoded := int64(base64.StdEncoding.DecodedLen(len(resolution.PostData)))
		if maximumDecoded > limit && maximumDecoded-limit > 2 {
			return resolution, fmt.Errorf("%w: request_body_too_large", network.ErrPolicyDenied)
		}
		decoded, err := base64.StdEncoding.Strict().DecodeString(resolution.PostData)
		if err != nil {
			return resolution, fmt.Errorf("%w: invalid_request_base64", network.ErrPolicyDenied)
		}
		length = int64(len(decoded))
	}
	if err := validateResolvedLength(headers, length); err != nil {
		return resolution, err
	}
	err = policy.ValidateRequest(ctx, target, method, headers.Get("Content-Type"), length, r.kind, r.request.SessionID)
	return resolution, err
}

func (r *fetchResolver) continuationHeaders(overrides []FetchHeader) (http.Header, error) {
	headers := make(http.Header, len(r.request.Headers))
	if overrides == nil {
		for key, value := range r.request.Headers {
			headers[key] = []string{value}
		}
		return headers, nil
	}
	for _, header := range overrides {
		key := http.CanonicalHeaderKey(header.Name)
		if key == "Host" || key == "Transfer-Encoding" || key == "Proxy-Authorization" || key == "Proxy-Connection" {
			return nil, fmt.Errorf("%w: transport_owned_header", network.ErrPolicyDenied)
		}
		headers[header.Name] = append(headers[header.Name], header.Value)
	}
	return network.ValidatedHeaders(headers)
}

func validateResolvedLength(headers http.Header, length int64) error {
	if values, supplied := headers["Content-Length"]; supplied {
		if len(values) != 1 {
			return fmt.Errorf("%w: request_length_mismatch", network.ErrPolicyDenied)
		}
		declared, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || declared < 0 || declared != length {
			return fmt.Errorf("%w: request_length_mismatch", network.ErrPolicyDenied)
		}
	}
	return nil
}

func validateFulfillment(ctx context.Context, policy *network.Policy, sessionID string, kind network.TargetKind, resolution FetchResolution) (FetchResolution, error) {
	headers := make(http.Header, len(resolution.ResponseHeaders))
	for _, header := range resolution.ResponseHeaders {
		headers[header.Name] = append(headers[header.Name], header.Value)
	}
	validated, err := network.ValidatedHeaders(headers)
	if err != nil {
		return resolution, err
	}
	if _, supplied := validated["Transfer-Encoding"]; supplied {
		return resolution, fmt.Errorf("%w: transport_owned_header", network.ErrPolicyDenied)
	}
	if err := validateResolvedLength(validated, int64(len(resolution.Body))); err != nil {
		return resolution, err
	}
	if resolution.StatusCode != 0 && (resolution.StatusCode < 200 || resolution.StatusCode > 599) {
		return resolution, fmt.Errorf("%w: invalid_fulfillment_status", network.ErrPolicyDenied)
	}
	if resolution.Body != "" && (resolution.StatusCode == 204 || resolution.StatusCode == 205 || resolution.StatusCode == 304) {
		return resolution, fmt.Errorf("%w: body_for_bodyless_status", network.ErrPolicyDenied)
	}
	err = policy.ValidateResponse(ctx, []byte(resolution.Body), validated.Get("Content-Encoding"), kind, sessionID)
	return resolution, err
}

func copyFetchHeaders(headers []FetchHeader) []FetchHeader {
	if headers == nil {
		return nil
	}
	return append([]FetchHeader{}, headers...)
}
