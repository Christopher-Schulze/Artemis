package cdpops

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

type elementCaller struct{ malformed bool }

func (c elementCaller) Call(_ context.Context, method string, _ any, result any) error {
	var raw []byte
	switch method {
	case "DOM.getDocument":
		raw = []byte(`{"root":{"nodeId":1}}`)
	case "DOM.querySelectorAll":
		raw = []byte(`{"nodeIds":[7]}`)
	case "DOM.describeNode":
		raw = []byte(`{"node":{"backendNodeId":42,"nodeName":"BUTTON","attributes":["id","submit"]}}`)
	case "DOM.getBoxModel":
		if c.malformed {
			raw = []byte(`{"model":{"content":[1,2],"padding":[1,2],"border":[1,2],"margin":[1,2],"width":100,"height":40}}`)
		} else {
			raw = []byte(`{"model":{"content":[10,20,110,20,110,60,10,60],"padding":[10,20,110,20,110,60,10,60],"border":[10,20,110,20,110,60,10,60],"margin":[10,20,110,20,110,60,10,60],"width":100,"height":40}}`)
		}
	default:
		return fmt.Errorf("unexpected method %s", method)
	}
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
