package observe

import (
	"sort"
)

func (c *Collector) diagnoseTarget(snapshot Snapshot, ref string, anchor *scopeAnchor) *TargetDiagnosis {
	if ref == "" {
		return nil
	}
	diagnosis := &TargetDiagnosis{Ref: ref, Status: ResolutionStale, State: ActionabilityUnavailable, Remedies: []string{"observe_again", "use_current_reference"}}
	c.mu.Lock()
	_, retained := c.refs[ref]
	c.mu.Unlock()
	if anchor == nil || !retained {
		return diagnosis
	}
	for _, node := range snapshot.Nodes {
		if snapshot.Epoch == anchor.epoch && node.FrameID == anchor.node.FrameID && node.BackendNodeID == anchor.node.BackendNodeID {
			diagnosis.Status, diagnosis.State = ResolutionExact, ClassifyActionability(&node)
			diagnosis.Remedies = targetRemedies(diagnosis.State)
			return diagnosis
		}
	}
	diagnosis.Status, diagnosis.State = ResolutionDetached, ClassifyMissingActionability(snapshot)
	if diagnosis.State == ActionabilityUnavailable {
		diagnosis.Status = ResolutionUnavailable
		return diagnosis
	}
	for _, node := range snapshot.Nodes {
		if node.FrameID != anchor.node.FrameID || node.Ref == "" {
			continue
		}
		score := similarity(anchor.node, node)
		if score >= c.config.ReResolveThreshold {
			diagnosis.Candidates = append(diagnosis.Candidates, TargetCandidate{Ref: node.Ref, Role: node.Role, Name: boundedUTF8(node.Name, 128), State: ClassifyActionability(&node), Confidence: score})
		}
	}
	return finalizeTargetCandidates(diagnosis)
}

func finalizeTargetCandidates(diagnosis *TargetDiagnosis) *TargetDiagnosis {
	sort.Slice(diagnosis.Candidates, func(i, j int) bool {
		if diagnosis.Candidates[i].Confidence == diagnosis.Candidates[j].Confidence {
			return diagnosis.Candidates[i].Ref < diagnosis.Candidates[j].Ref
		}
		return diagnosis.Candidates[i].Confidence > diagnosis.Candidates[j].Confidence
	})
	if len(diagnosis.Candidates) > 1 && diagnosis.Candidates[0].Confidence-diagnosis.Candidates[1].Confidence < .05 {
		diagnosis.Status, diagnosis.State = ResolutionAmbiguous, ActionabilityUnavailable
		diagnosis.Remedies = []string{"narrow_scope", "choose_observed_reference"}
	} else if len(diagnosis.Candidates) > 0 {
		diagnosis.Status, diagnosis.State = ResolutionReResolved, diagnosis.Candidates[0].State
		diagnosis.Remedies = []string{"verify_semantic_replacement", "use_current_reference"}
	}
	if len(diagnosis.Candidates) > 3 {
		diagnosis.Candidates = diagnosis.Candidates[:3]
	}
	return diagnosis
}

func targetRemedies(state ActionabilityState) []string {
	switch state {
	case ActionabilityReady:
		return nil
	case ActionabilityCovered:
		return []string{"inspect_covering_element", "dismiss_authorized_overlay", "observe_again"}
	case ActionabilityHitUnknown, ActionabilityNoLayout:
		return []string{"scroll_target_into_view", "wait_for_layout", "observe_again"}
	case ActionabilityDisabled:
		return []string{"check_required_inputs", "wait_for_enabled_state"}
	case ActionabilityHidden:
		return []string{"inspect_visibility", "observe_again"}
	default:
		return []string{"observe_again", "choose_observed_reference"}
	}
}
