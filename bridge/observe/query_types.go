package observe

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type QueryOptions struct {
	Mode       Mode     `json:"mode,omitempty"`
	SubtreeRef string   `json:"subtreeRef,omitempty"`
	TargetRef  string   `json:"targetRef,omitempty"`
	FrameID    string   `json:"frameId,omitempty"`
	Roles      []string `json:"roles,omitempty"`
	Text       string   `json:"text,omitempty"`
	MaxNodes   int      `json:"maxNodes,omitempty"`
	MaxBytes   int      `json:"maxBytes,omitempty"`
}

type QueryRequest struct {
	Options      QueryOptions `json:"options"`
	BaseRevision string       `json:"baseRevision,omitempty"`
}

type QueryKind string

const (
	QueryFull      QueryKind = "full"
	QueryUnchanged QueryKind = "unchanged"
	QueryDelta     QueryKind = "delta"
)

type TargetCandidate struct {
	Ref        string             `json:"ref"`
	Role       string             `json:"role"`
	Name       string             `json:"name"`
	State      ActionabilityState `json:"state"`
	Confidence float64            `json:"confidence"`
}

type TargetDiagnosis struct {
	Ref        string             `json:"ref"`
	Status     ResolutionStatus   `json:"status"`
	State      ActionabilityState `json:"state"`
	Candidates []TargetCandidate  `json:"candidates,omitempty"`
	Remedies   []string           `json:"remedies,omitempty"`
}

type QueryResult struct {
	Schema            string           `json:"schema"`
	Kind              QueryKind        `json:"kind"`
	Revision          string           `json:"revision"`
	BaseRevision      string           `json:"baseRevision,omitempty"`
	Epoch             uint64           `json:"epoch"`
	CapturedAt        time.Time        `json:"capturedAt"`
	NodeCount         int              `json:"nodeCount"`
	Warnings          []string         `json:"warnings,omitempty"`
	Truncated         bool             `json:"truncated"`
	TruncationReasons []string         `json:"truncationReasons,omitempty"`
	Snapshot          *Snapshot        `json:"snapshot,omitempty"`
	Delta             *Diff            `json:"delta,omitempty"`
	Target            *TargetDiagnosis `json:"target,omitempty"`
	Lost              bool             `json:"lost"`
	ResetReason       string           `json:"resetReason,omitempty"`
}

func (result QueryResult) Validate() error {
	if result.Schema != QuerySchema || result.Revision == "" || result.Epoch == 0 || result.CapturedAt.IsZero() || result.NodeCount < 0 {
		return fmt.Errorf("query: invalid schema/revision/epoch")
	}
	switch result.Kind {
	case QueryFull:
		if result.Snapshot == nil || result.Delta != nil {
			return fmt.Errorf("query: full response requires a snapshot and no delta")
		}
		snapshot := result.Snapshot
		if snapshot.Schema != Schema || snapshot.Epoch != result.Epoch || !snapshot.CapturedAt.Equal(result.CapturedAt) || snapshot.TotalNodes != result.NodeCount || snapshot.TotalNodes < len(snapshot.Nodes) || snapshot.Truncated != result.Truncated || !slices.Equal(snapshot.Warnings, result.Warnings) || !slices.Equal(snapshot.TruncationReasons, result.TruncationReasons) || result.Truncated && !result.Lost {
			return fmt.Errorf("query: inconsistent full observation evidence")
		}
	case QueryDelta:
		if result.Snapshot != nil || result.Delta == nil || result.BaseRevision == "" || result.BaseRevision == result.Revision || result.Lost || result.Truncated {
			return fmt.Errorf("query: invalid or incomplete delta")
		}
	case QueryUnchanged:
		if result.Snapshot != nil || result.Delta != nil || result.BaseRevision != result.Revision || result.Lost || result.Truncated {
			return fmt.Errorf("query: invalid unchanged response")
		}
	default:
		return fmt.Errorf("query: unsupported response kind")
	}
	if result.Kind != QueryFull && result.ResetReason != "" {
		return fmt.Errorf("query: incremental response cannot reset its baseline")
	}
	return nil
}

func (c *Collector) normalizeQuery(options QueryOptions) (QueryOptions, error) {
	if options.Mode == "" {
		options.Mode = ModeInteractive
	}
	if options.Mode != ModeFull && options.Mode != ModeInteractive && options.Mode != ModeSubtree && options.Mode != ModeEvidence {
		return QueryOptions{}, fmt.Errorf("query: unsupported observation mode %q", options.Mode)
	}
	options.SubtreeRef, options.TargetRef = normalizeRef(options.SubtreeRef), normalizeRef(options.TargetRef)
	if (options.Mode == ModeSubtree) != (options.SubtreeRef != "") {
		return QueryOptions{}, fmt.Errorf("query: subtree mode requires a subtree ref and other modes forbid it")
	}
	if options.MaxNodes == 0 {
		options.MaxNodes = min(200, c.config.MaxNodes)
	}
	if options.MaxBytes == 0 {
		options.MaxBytes = min(32768, c.config.MaxSnapshotBytes)
	}
	if options.MaxNodes < 1 || options.MaxNodes > c.config.MaxNodes || options.MaxBytes < 512 || options.MaxBytes > c.config.MaxSnapshotBytes {
		return QueryOptions{}, fmt.Errorf("query: node/byte budgets exceed collector bounds or cannot fit metadata")
	}
	return normalizeQueryText(options)
}

func normalizeQueryText(options QueryOptions) (QueryOptions, error) {
	if len(options.Roles) > 32 || len(options.Text) > 256 || len(options.FrameID) > 128 || len(options.TargetRef) > 128 || len(options.SubtreeRef) > 128 || !utf8.ValidString(options.Text) || !utf8.ValidString(options.FrameID) || !utf8.ValidString(options.TargetRef) || !utf8.ValidString(options.SubtreeRef) {
		return QueryOptions{}, fmt.Errorf("query: selector exceeds bounded text/role limits")
	}
	options.Roles = append([]string(nil), options.Roles...)
	for i, role := range options.Roles {
		if role == "" || len(role) > 64 || !utf8.ValidString(role) {
			return QueryOptions{}, fmt.Errorf("query: invalid role")
		}
		options.Roles[i] = strings.ToLower(role)
	}
	slices.Sort(options.Roles)
	options.Roles = slices.Compact(options.Roles)
	options.Text = strings.ToLower(options.Text)
	return options, nil
}

func boundedUTF8(value string, bytes int) string {
	if len(value) <= bytes {
		return value
	}
	for bytes > 0 && !utf8.RuneStart(value[bytes]) {
		bytes--
	}
	return value[:bytes]
}
