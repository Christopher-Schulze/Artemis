package pool

import (
	"strings"
	"testing"
)

func TestBufferKeepsCapacityAndNeverAliasesOutput(t *testing.T) {
	var b Buffer
	b.WriteString("hello ")
	b.WriteByte('w')
	b.WriteRune('ö')
	if _, err := b.Write([]byte("rld")); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if out != "hello wörld" || b.Len() != len(out) || !b.HasSuffix("rld") || b.HasSuffix("x") || b.HasSuffix(strings.Repeat("y", 64)) {
		t.Fatalf("buffer content = %q", out)
	}
	before := b.Cap()
	b.Reset()
	if b.Len() != 0 || b.Cap() != before || before == 0 {
		t.Fatalf("Reset discarded the backing array: cap %d -> %d", before, b.Cap())
	}
	b.WriteString("XXXXXXXXXXX")
	if out != "hello wörld" {
		t.Fatalf("String result aliases the reused buffer: %q", out)
	}
	b.Grow(1000)
	if b.Cap() < b.Len()+1000 || b.String() != "XXXXXXXXXXX" {
		t.Fatalf("Grow lost content or capacity: %d %q", b.Cap(), b.String())
	}
}

func TestPutBufferWipesPageBytesAndBoundsRetention(t *testing.T) {
	b := GetBuffer()
	b.WriteString("secret page text")
	view := b.Bytes()[:cap(b.Bytes())]
	PutBuffer(b)
	for _, value := range view {
		if value != 0 {
			t.Fatalf("returned buffer still holds page bytes: %q", view)
		}
	}
	oversized := &Buffer{}
	oversized.Grow(MaxRetainedBytes + 1)
	oversized.WriteString("kept")
	PutBuffer(oversized)
	if oversized.String() != "kept" {
		t.Fatal("an oversized buffer entered the pool (it was wiped)")
	}
	PutBuffer(nil)
}

func TestPooledDumpCycleAllocatesOnlyTheResultCopy(t *testing.T) {
	payload := strings.Repeat("<p>governed payload</p>", 256)
	warm := GetBuffer()
	warm.WriteString(payload)
	PutBuffer(warm)
	allocations := testing.AllocsPerRun(200, func() {
		b := GetBuffer()
		b.WriteString(payload)
		_ = b.String()
		PutBuffer(b)
	})
	if allocations > 1.05 {
		t.Fatalf("pooled cycle allocates %.2f times per run; growth storage was not reused", allocations)
	}
}

func TestConcurrentPooledBuffersStayIndependent(t *testing.T) {
	done := make(chan string, 64)
	for worker := 0; worker < 64; worker++ {
		go func(worker int) {
			b := GetBuffer()
			want := strings.Repeat(string(rune('a'+worker%26)), 512+worker)
			b.WriteString(want)
			got := b.String()
			PutBuffer(b)
			if got != want {
				done <- got
				return
			}
			done <- ""
		}(worker)
	}
	for worker := 0; worker < 64; worker++ {
		if got := <-done; got != "" {
			t.Fatalf("pooled buffer shared between users: %.32q", got)
		}
	}
}
