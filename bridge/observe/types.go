package observe

import (
	"context"
	"time"
)

// Caller is the exact CDP surface required by the observation pipeline.
// The transport remains generic because CDP methods have different request
// and response structs; known observe calls pass concrete protocol types at
// their call sites, while genuinely dynamic values use json.RawMessage.
type Caller interface {
	Call(context.Context, string, any, any) error
}

type FrameCaller interface {
	Caller
	FrameSessions() map[string]string
	CallFrame(context.Context, string, string, any, any) error
}

type Mode string

const (
	ModeFull        Mode = "full"
	ModeInteractive Mode = "interactive"
	ModeSubtree     Mode = "subtree"
	ModeEvidence    Mode = "evidence"
)

type Config struct {
	MaxNodes            int
	MaxDepth            int
	MaxTextBytes        int
	MaxSnapshotBytes    int
	MaxHitTests         int
	SensitiveAttributes []string
	ReResolveThreshold  float64
}

func DefaultConfig() Config {
	return Config{MaxNodes: 2_000, MaxDepth: 64, MaxTextBytes: 4_096, MaxSnapshotBytes: 1_000_000, MaxHitTests: 256, SensitiveAttributes: []string{"authorization", "password", "secret", "token", "api-key", "apikey"}, ReResolveThreshold: 0.82}
}

type Rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type HitState string

const (
	HitClear   HitState = "clear"
	HitCovered HitState = "covered"
	HitUnknown HitState = "unknown"
)

type Node struct {
	Ref                 string            `json:"ref,omitempty"`
	BackendNodeID       int64             `json:"backendNodeId"`
	FrameID             string            `json:"frameId"`
	ParentBackendNodeID int64             `json:"parentBackendNodeId,omitempty"`
	Role                string            `json:"role"`
	Name                string            `json:"name"`
	Value               string            `json:"value,omitempty"`
	Tag                 string            `json:"tag,omitempty"`
	Attributes          map[string]string `json:"attributes,omitempty"`
	Depth               int               `json:"depth"`
	Order               int               `json:"order"`
	ShadowPath          []string          `json:"shadowPath,omitempty"`
	Box                 *Rect             `json:"box,omitempty"`
	Visible             bool              `json:"visible"`
	Disabled            bool              `json:"disabled,omitempty"`
	Focused             bool              `json:"focused,omitempty"`
	Interactive         bool              `json:"interactive"`
	Interactable        bool              `json:"interactable"`
	Hit                 HitState          `json:"hit"`
}

type Snapshot struct {
	Schema            string    `json:"schema"`
	Epoch             uint64    `json:"epoch"`
	CapturedAt        time.Time `json:"capturedAt"`
	Mode              Mode      `json:"mode"`
	RootBackendNodeID int64     `json:"rootBackendNodeId"`
	Nodes             []Node    `json:"nodes"`
	TotalNodes        int       `json:"totalNodes"`
	Truncated         bool      `json:"truncated"`
	TruncationReasons []string  `json:"truncationReasons,omitempty"`
	Warnings          []string  `json:"warnings,omitempty"`
}

type ResolutionStatus string

const (
	ResolutionExact      ResolutionStatus = "exact"
	ResolutionReResolved ResolutionStatus = "re_resolved"
	ResolutionStale      ResolutionStatus = "stale"
	ResolutionDetached   ResolutionStatus = "detached"
	ResolutionAmbiguous  ResolutionStatus = "ambiguous"
	ResolutionCrossFrame ResolutionStatus = "cross_frame"
)

type Resolution struct {
	Status     ResolutionStatus `json:"status"`
	Node       *Node            `json:"node,omitempty"`
	Confidence float64          `json:"confidence,omitempty"`
	Reason     string           `json:"reason,omitempty"`
}

type Diff struct {
	Added   []Node `json:"added"`
	Changed []Node `json:"changed"`
	Removed []Node `json:"removed"`
}
