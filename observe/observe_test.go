package observe

import "testing"

func TestNetworkRingBufferCap(t *testing.T) {
	rb := NewNetworkRingBuffer(2)
	rb.Push(NetworkEvent{URL: "a"})
	rb.Push(NetworkEvent{URL: "b"})
	rb.Push(NetworkEvent{URL: "c"})
	if rb.Len() != 2 || rb.Snapshot()[0].URL != "b" {
		t.Fatalf("snap=%v", rb.Snapshot())
	}
}

func TestAXTreeMyersDiff(t *testing.T) {
	before := []AXNode{{ID: "1", Role: "button", Name: "ok"}}
	after := []AXNode{{ID: "2", Role: "button", Name: "ok"}}
	if MyersDiff(before, after) == 0 {
		t.Fatal("id change must produce diff")
	}
}

func TestRoleSnapshotDedup(t *testing.T) {
	nodes := []AXNode{{Role: "button", Name: "x"}, {Role: "button", Name: "x"}}
	out := DedupRoleSnapshot(nodes)
	if len(out) != 1 {
		t.Fatalf("len=%d", len(out))
	}
}
