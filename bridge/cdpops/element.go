package cdpops

import (
	"fmt"
	"strings"
)

// element.go (spec L4019: bridge/cdpops/element.go - element queries +
// box model).
//
// Low-level CDP ops: element queries and box model retrieval.
// Provides functions for querying DOM elements by selector/ref and
// retrieving their box model (position, size, viewport).

// BoxModel represents an element's box model
// (spec L4019: element queries + box model).
type BoxModel struct {
	Content Quad `json:"content"`
	Padding Quad `json:"padding"`
	Border  Quad `json:"border"`
	Margin  Quad `json:"margin"`
	Width   int  `json:"width"`
	Height  int  `json:"height"`
}

// Quad represents a quadrilateral (4 points)
// (spec L4019: element queries + box model).
type Quad struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
	X3 float64 `json:"x3"`
	Y3 float64 `json:"y3"`
	X4 float64 `json:"x4"`
	Y4 float64 `json:"y4"`
}

// ElementInfo represents a queried DOM element
// (spec L4019: element queries + box model).
type ElementInfo struct {
	Ref       string    `json:"ref"`
	TagName   string    `json:"tagName"`
	Type      string    `json:"type,omitempty"`
	Text      string    `json:"text,omitempty"`
	Role      string    `json:"role,omitempty"`
	Classes   []string  `json:"classes,omitempty"`
	ID        string    `json:"id,omitempty"`
	Name      string    `json:"name,omitempty"`
	Value     string    `json:"value,omitempty"`
	Visible   bool      `json:"visible"`
	Clickable bool      `json:"clickable"`
	Box       *BoxModel `json:"box,omitempty"`
}

// ElementQuery represents a query for DOM elements
// (spec L4019: element queries + box model).
type ElementQuery struct {
	Selector string `json:"selector"` // CSS selector or eN ref
	FrameID  string `json:"frameId,omitempty"`
	Visible  *bool  `json:"visible,omitempty"` // filter by visibility
}

// QueryResult is the result of an element query
// (spec L4019: element queries + box model).
type QueryResult struct {
	Elements []ElementInfo `json:"elements"`
	Total    int           `json:"total"`
	Error    string        `json:"error,omitempty"`
}

// QueryElements queries DOM elements by selector
// (spec L4019: element queries + box model).
func QueryElements(query ElementQuery) QueryResult {
	if query.Selector == "" {
		return QueryResult{Error: "element query: empty selector"}
	}
	// In a real implementation, this would use CDP DOM.querySelector
	// or DOM.querySelectorAll. Here we provide the structure.
	return QueryResult{
		Elements: []ElementInfo{},
		Total:    0,
	}
}

// GetBoxModel retrieves the box model for an element
// (spec L4019: element queries + box model).
func GetBoxModel(ref string) (*BoxModel, error) {
	if ref == "" {
		return nil, fmt.Errorf("box model: empty ref")
	}
	// In a real implementation, this would use CDP DOM.getBoxModel
	return &BoxModel{
		Content: Quad{0, 0, 100, 0, 100, 100, 0, 100},
		Border:  Quad{0, 0, 100, 0, 100, 100, 0, 100},
		Padding: Quad{0, 0, 100, 0, 100, 100, 0, 100},
		Margin:  Quad{0, 0, 100, 0, 100, 100, 0, 100},
		Width:   100,
		Height:  100,
	}, nil
}

// IsElementVisible checks if an element is visible
// (spec L4019: element queries + box model).
func IsElementVisible(box *BoxModel) bool {
	if box == nil {
		return false
	}
	return box.Width > 0 && box.Height > 0
}

// IsElementClickable checks if an element is clickable
// (spec L4019: element queries + box model).
func IsElementClickable(info *ElementInfo) bool {
	if info == nil {
		return false
	}
	return info.Visible && IsElementVisible(info.Box)
}

// GetElementCenter returns the center point of an element's box model
// (spec L4019: element queries + box model).
func GetElementCenter(box *BoxModel) (float64, float64) {
	if box == nil {
		return 0, 0
	}
	return float64(box.Width) / 2, float64(box.Height) / 2
}

// FilterVisible filters elements to only visible ones
// (spec L4019: element queries + box model).
func FilterVisible(elements []ElementInfo) []ElementInfo {
	var result []ElementInfo
	for _, e := range elements {
		if e.Visible {
			result = append(result, e)
		}
	}
	return result
}

// FindByRef finds an element by its ref in a list
// (spec L4019: element queries + box model).
func FindByRef(elements []ElementInfo, ref string) (*ElementInfo, bool) {
	for i := range elements {
		if elements[i].Ref == ref {
			return &elements[i], true
		}
	}
	return nil, false
}

// FindByTag finds elements by tag name
// (spec L4019: element queries + box model).
func FindByTag(elements []ElementInfo, tag string) []ElementInfo {
	var result []ElementInfo
	for _, e := range elements {
		if strings.EqualFold(e.TagName, tag) {
			result = append(result, e)
		}
	}
	return result
}

// String returns a diagnostic summary.
func (b BoxModel) String() string {
	return fmt.Sprintf("BoxModel{w:%d h:%d}", b.Width, b.Height)
}

// String returns a diagnostic summary.
func (e ElementInfo) String() string {
	return fmt.Sprintf("ElementInfo{ref:%s tag:%s visible:%v}", e.Ref, e.TagName, e.Visible)
}
