package actions

import "testing"

func TestFormIntentValidateAndPrefetchKey(t *testing.T) {
	f := FormIntent{
		ActionURL: "https://example.com/submit",
		Method:    "POST",
		Fields:    []FormField{{Name: "email", Value: "a@b.c"}},
		Prefetch:  true,
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.PrefetchKey() == "" {
		t.Fatal("prefetch key required")
	}
}
