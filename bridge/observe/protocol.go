package observe

import (
	"encoding/json"
	"strings"
)

type stringIndex int

type rareStringData struct {
	Index []int         `json:"index"`
	Value []stringIndex `json:"value"`
}
type rareBooleanData struct {
	Index []int `json:"index"`
}

type nodeTreeSnapshot struct {
	ParentIndex    []int            `json:"parentIndex"`
	NodeType       []int            `json:"nodeType"`
	NodeName       []stringIndex    `json:"nodeName"`
	NodeValue      []stringIndex    `json:"nodeValue"`
	BackendNodeID  []int64          `json:"backendNodeId"`
	Attributes     [][]stringIndex  `json:"attributes"`
	ShadowRootType *rareStringData  `json:"shadowRootType,omitempty"`
	InputValue     *rareStringData  `json:"inputValue,omitempty"`
	InputChecked   *rareBooleanData `json:"inputChecked,omitempty"`
}

type layoutTreeSnapshot struct {
	NodeIndex []int           `json:"nodeIndex"`
	Styles    [][]stringIndex `json:"styles"`
	Bounds    [][]float64     `json:"bounds"`
}

type documentSnapshot struct {
	FrameID stringIndex        `json:"frameId"`
	Nodes   nodeTreeSnapshot   `json:"nodes"`
	Layout  layoutTreeSnapshot `json:"layout"`
}

type domSnapshotResult struct {
	Documents []documentSnapshot `json:"documents"`
	Strings   []string           `json:"strings"`
}

type emptyParams struct{}

type emptyResult struct{}

type captureSnapshotParams struct {
	ComputedStyles    []string `json:"computedStyles"`
	IncludeDOMRects   bool     `json:"includeDOMRects"`
	IncludePaintOrder bool     `json:"includePaintOrder"`
}

type getDocumentParams struct {
	Depth  int  `json:"depth"`
	Pierce bool `json:"pierce"`
}

type getAXTreeParams struct {
	FrameID string `json:"frameId"`
}

type getNodeForLocationParams struct {
	X                         int  `json:"x"`
	Y                         int  `json:"y"`
	IncludeUserAgentShadowDOM bool `json:"includeUserAgentShadowDOM"`
	IgnorePointerEventsNone   bool `json:"ignorePointerEventsNone"`
}

type getNodeForLocationResult struct {
	BackendNodeID int64 `json:"backendNodeId"`
}

type axValue struct {
	Value json.RawMessage `json:"value"`
}
type axProperty struct {
	Name  string  `json:"name"`
	Value axValue `json:"value"`
}
type axNode struct {
	BackendNodeID int64        `json:"backendDOMNodeId"`
	Ignored       bool         `json:"ignored"`
	Role          axValue      `json:"role"`
	Name          axValue      `json:"name"`
	Value         axValue      `json:"value"`
	Properties    []axProperty `json:"properties"`
}
type axTreeResult struct {
	Nodes []axNode `json:"nodes"`
}

type frame struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
}
type frameTree struct {
	Frame       frame       `json:"frame"`
	ChildFrames []frameTree `json:"childFrames,omitempty"`
}
type frameTreeResult struct {
	FrameTree frameTree `json:"frameTree"`
}

type domNode struct {
	BackendNodeID  int64     `json:"backendNodeId"`
	ShadowRootType string    `json:"shadowRootType,omitempty"`
	Children       []domNode `json:"children,omitempty"`
	ShadowRoots    []domNode `json:"shadowRoots,omitempty"`
}

type documentResult struct {
	Root domNode `json:"root"`
}

func (v axValue) String() string {
	raw := strings.TrimSpace(string(v.Value))
	if raw == "" || raw == "null" {
		return ""
	}
	var value string
	if err := json.Unmarshal(v.Value, &value); err == nil {
		return value
	}
	return strings.Trim(raw, `"`)
}

func (v axValue) Bool() bool {
	var value bool
	return json.Unmarshal(v.Value, &value) == nil && value
}

func stringAt(values []string, index stringIndex) string {
	i := int(index)
	if i < 0 || i >= len(values) {
		return ""
	}
	return values[i]
}

func rareStringAt(data *rareStringData, index int, values []string) string {
	if data == nil {
		return ""
	}
	for i, candidate := range data.Index {
		if candidate == index && i < len(data.Value) {
			return stringAt(values, data.Value[i])
		}
	}
	return ""
}
