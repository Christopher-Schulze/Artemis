package pool

import "testing"

func TestBuilderPoolReturnsResetBuilder(t *testing.T) {
	b := GetBuilder()
	b.WriteString("stale")
	PutBuilder(b)

	reused := GetBuilder()
	if reused.Len() != 0 {
		t.Fatalf("builder length = %d, want 0", reused.Len())
	}
	PutBuilder(reused)
}

func TestPutBuilderAcceptsNilAndLargeBuilder(t *testing.T) {
	PutBuilder(nil)

	b := GetBuilder()
	b.Grow(1<<20 + 1)
	PutBuilder(b)

	fresh := GetBuilder()
	if fresh.Len() != 0 {
		t.Fatalf("builder length = %d, want 0", fresh.Len())
	}
	PutBuilder(fresh)
}
