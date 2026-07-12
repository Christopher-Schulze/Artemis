package serve

import (
	"encoding/json"
	"testing"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

func TestObservationEventCanonicalSchema(t *testing.T) {
	snapshot := bridgeobserve.Snapshot{Schema: bridgeobserve.Schema, Nodes: []bridgeobserve.Node{{Ref: "e1", BackendNodeID: 7}}}
	event, err := ObservationEvent(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != StreamEventSnapshot {
		t.Fatalf("type=%s", event.Type)
	}
	var decoded bridgeobserve.Snapshot
	if err = json.Unmarshal(event.Data, &decoded); err != nil || decoded.Schema != bridgeobserve.Schema || decoded.Nodes[0].Ref != "e1" {
		t.Fatalf("decoded=%#v err=%v", decoded, err)
	}
}
func TestObservationEventRejectsSchemaDrift(t *testing.T) {
	if _, err := ObservationEvent(bridgeobserve.Snapshot{Schema: "other"}); err == nil {
		t.Fatal("schema drift accepted")
	}
}
