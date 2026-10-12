package bridge

import "testing"

func TestCDPContextTreeEnforcesHierarchyAndRecursiveDetach(t *testing.T) {
	tree := NewCDPContextTree()
	for _, unit := range []CDPContextUnit{
		{ID: "alloc", Kind: ContextKindAlloc},
		{ID: "browser", ParentID: "alloc", Kind: ContextKindBrowser},
		{ID: "tab", ParentID: "browser", Kind: ContextKindTab},
	} {
		if err := tree.Attach(unit); err != nil {
			t.Fatal(err)
		}
	}
	if tree.Len() != 3 {
		t.Fatalf("len=%d", tree.Len())
	}
	removed, err := tree.Detach("browser")
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 || tree.Len() != 1 {
		t.Fatalf("removed=%v len=%d", removed, tree.Len())
	}
	if _, err := tree.Hierarchy("tab"); err == nil {
		t.Fatal("detached descendant remained routable")
	}
}

func TestCDPContextTreeRejectsDuplicateAndIllegalParents(t *testing.T) {
	tree := NewCDPContextTree()
	if err := tree.Attach(CDPContextUnit{ID: "alloc", Kind: ContextKindAlloc}); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []CDPContextUnit{
		{ID: "alloc", Kind: ContextKindAlloc},
		{ID: "second-root", Kind: ContextKindAlloc},
		{ID: "tab", ParentID: "alloc", Kind: ContextKindTab},
		{ID: "invalid", ParentID: "alloc", Kind: ContextKind("worker")},
	} {
		if err := tree.Attach(unit); err == nil {
			t.Fatalf("illegal context unit accepted: %+v", unit)
		}
	}
}
