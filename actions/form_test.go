package actions

import "testing"

func TestFormIntentValidateAndIdentityExcludeValues(t *testing.T) {
	f := FormIntent{
		SessionID: "session-1",
		PageID:    "page-1",
		FormRoot:  "#login",
		Fields:    []FormField{{Name: "email", Value: "a@b.c", Selector: "#email"}},
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	identity := f.Identity()
	f.Fields[0].Value = "different-secret"
	if identity != f.Identity() {
		t.Fatal("field value changed cache identity")
	}
}

func TestFormIntentIdentityIsDelimiterCollisionSafe(t *testing.T) {
	left := FormIntent{SessionID: "a|b", PageID: "c", FormRoot: "d", Fields: []FormField{{Name: "x", Selector: "#x"}}}
	right := FormIntent{SessionID: "a", PageID: "b|c", FormRoot: "d", Fields: []FormField{{Name: "x", Selector: "#x"}}}
	if err := left.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := right.Validate(); err != nil {
		t.Fatal(err)
	}
	if left.Identity() == right.Identity() {
		t.Fatal("distinct structured identities collided")
	}
}
