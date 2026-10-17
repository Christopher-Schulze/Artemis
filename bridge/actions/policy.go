package actions

import (
	"context"
	"fmt"

	"github.com/Christopher-Schulze/Artemis/bridge"
	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func (r *Runtime) authorizeNativeAction(ctx context.Context, request Request) error {
	policyRequest, err := r.PolicyRequest(ctx, request)
	if err != nil {
		return err
	}
	return r.authorizePolicyRequest(ctx, policyRequest)
}

func (r *Runtime) authorizePolicyRequest(ctx context.Context, request bridge.PolicyRequest) error {
	response, err := r.policyHook.Check(ctx, request)
	if err != nil {
		return err
	}
	if response.Decision != bridge.PolicyDecisionAllow {
		return &bridge.PolicyError{Decision: response.Decision, Reason: response.Reason}
	}
	return nil
}

// PolicyRequest derives actual owning document realms for a trusted host gate.
// It performs no action dispatch and never uses caller URL claims for interactions.
func (r *Runtime) PolicyRequest(ctx context.Context, request Request) (bridge.PolicyRequest, error) {
	if r == nil || r.page == nil || ctx == nil {
		return bridge.PolicyRequest{}, fmt.Errorf("action policy: runtime and context required")
	}
	policyRequest := bridge.PolicyRequest{Action: string(request.Kind)}
	if request.Kind == KindDialog {
		dialog, ok := r.page.PendingDialog()
		if !ok || request.DialogGeneration != 0 && request.DialogGeneration != dialog.Generation {
			return bridge.PolicyRequest{}, fmt.Errorf("action policy: observed dialog unavailable")
		}
		policyRequest.URL = dialog.URL
		return policyRequest, nil
	}
	if request.Kind == KindNavigate || request.Kind == KindTabOpen {
		policyRequest.URL, policyRequest.Method = request.URL, "GET"
	} else {
		frames, err := r.policyFrames(ctx, request)
		if err != nil {
			return bridge.PolicyRequest{}, err
		}
		for _, frameID := range frames {
			target, err := r.page.FramePolicyURL(ctx, frameID)
			if err != nil {
				return bridge.PolicyRequest{}, err
			}
			if policyRequest.URL == "" {
				policyRequest.URL = target
			} else {
				policyRequest.TargetURLs = append(policyRequest.TargetURLs, target)
			}
		}
	}
	if (request.Kind == KindTabClose || request.Kind == KindTabSwitch) && request.TargetID != "" {
		tab, ok, err := r.tabs.GetTabContext(ctx, request.TargetID)
		if err != nil {
			return bridge.PolicyRequest{}, err
		}
		if !ok {
			return bridge.PolicyRequest{}, fmt.Errorf("action policy: owned target unavailable")
		}
		policyRequest.TargetURLs = append(policyRequest.TargetURLs, tab.URL)
	}
	return policyRequest, nil
}

func (r *Runtime) authorizeHistoryTarget(ctx context.Context, kind Kind, target string) error {
	if r.policyHook == nil {
		return nil // Trusted low-level callers own their authority.
	}
	current, err := r.page.FramePolicyURL(ctx, "")
	if err != nil {
		return err
	}
	return r.authorizePolicyRequest(ctx, bridge.PolicyRequest{Action: string(kind), URL: current, TargetURLs: []string{target}})
}

func (r *Runtime) policyFrames(ctx context.Context, request Request) ([]string, error) {
	frames := []string{""}
	if request.FrameID != "" {
		frames = append(frames, request.FrameID)
	}
	for _, ref := range []string{request.Ref, request.TargetRef} {
		if ref == "" {
			continue
		}
		resolution, err := r.observer.Resolve(ctx, ref)
		if err != nil {
			return nil, err
		}
		if resolution.Node == nil || (resolution.Status != bridgeobserve.ResolutionExact && resolution.Status != bridgeobserve.ResolutionReResolved) || resolution.Node.FrameID == "" {
			return nil, fmt.Errorf("action policy: reference realm unavailable")
		}
		found := false
		for _, known := range frames {
			if known == resolution.Node.FrameID {
				found = true
				break
			}
		}
		if !found {
			frames = append(frames, resolution.Node.FrameID)
		}
	}
	return frames, nil
}
