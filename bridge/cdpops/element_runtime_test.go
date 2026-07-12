package cdpops

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type elementCaller struct{ malformed bool }

func (c elementCaller) Call(_ context.Context, method string, _ any, result any) error {
	var value any
	switch method {
	case "DOM.getDocument":
		value = map[string]any{"root": map[string]any{"nodeId": 1}}
	case "DOM.querySelectorAll":
		value = map[string]any{"nodeIds": []int{7}}
	case "DOM.describeNode":
		value = map[string]any{"node": map[string]any{"backendNodeId": 42, "nodeName": "BUTTON", "attributes": []string{"id", "submit"}}}
	case "DOM.getBoxModel":
		quad := []float64{10, 20, 110, 20, 110, 60, 10, 60}
		if c.malformed {
			quad = []float64{1, 2}
		}
		value = map[string]any{"model": map[string]any{"content": quad, "padding": quad, "border": quad, "margin": quad, "width": 100, "height": 40}}
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
	raw, _ := json.Marshal(value)
	return json.Unmarshal(raw, result)
}

func TestElementClientUsesRealCDPGeometry(t *testing.T) {
	client, err := NewElementClient(elementCaller{})
	if err != nil {
		t.Fatal(err)
	}
	elements, err := client.QuerySelector(context.Background(), "button")
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].Box.Width != 100 || elements[0].Box.Content.X1 != 10 || elements[0].ID != "submit" {
		t.Fatalf("unexpected elements: %#v", elements)
	}
	x, y := GetElementCenter(elements[0].Box)
	if x != 60 || y != 40 {
		t.Fatalf("center=(%v,%v), want (60,40)", x, y)
	}
}
func TestElementClientRejectsMalformedGeometry(t *testing.T) {
	client, _ := NewElementClient(elementCaller{malformed: true})
	if _, err := client.GetBoxModel(context.Background(), 42); err == nil {
		t.Fatal("malformed CDP quad accepted")
	}
}
