package artemis

import "testing"

func TestProfileIsolation(t *testing.T) {
	reg := NewProfileRegistry()
	a, err := reg.Create("profile-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := reg.Create("profile-b")
	if err != nil || a.Seed == b.Seed {
		t.Fatalf("a=%+v b=%+v err=%v", a, b, err)
	}
}

func TestAutoLoginSessions(t *testing.T) {
	reg := NewAutoLoginRegistry()
	if err := reg.Put(AutoLoginSession{ProfileID: "p1", ContextID: "ctx-1"}); err != nil {
		t.Fatal(err)
	}
	got, ok := reg.Get("p1")
	if !ok || got.ContextID != "ctx-1" {
		t.Fatalf("got=%+v ok=%v", got, ok)
	}
}
