package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
)

type humanEventCounts struct {
	Moves, Downs, Ups, KeyDowns, KeyUps, Backspaces, Wheels int
}

func readHumanCounts(t *testing.T, f *actionFixture) humanEventCounts {
	t.Helper()
	out := f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: "JSON.stringify(window.__human)"})
	requireAction(t, out)
	text, ok := out.Value.(string)
	if !ok {
		t.Fatalf("event counters unavailable: %#v", out.Value)
	}
	var counts humanEventCounts
	if err := json.Unmarshal([]byte(text), &counts); err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestRuntimeHumanProfileEmitsRealEventSequences(t *testing.T) {
	f := newActionFixture(t)
	defer f.close()
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindEvaluate, Expression: `(()=>{const c=window.__human={Moves:0,Downs:0,Ups:0,KeyDowns:0,KeyUps:0,Backspaces:0,Wheels:0};
		addEventListener('mousemove',()=>c.Moves++,true);addEventListener('mousedown',()=>c.Downs++,true);addEventListener('mouseup',()=>c.Ups++,true);
		addEventListener('keydown',e=>{c.KeyDowns++;if(e.key==='Backspace')c.Backspaces++},true);addEventListener('keyup',()=>c.KeyUps++,true);
		addEventListener('wheel',()=>c.Wheels++,true);return true})()`}))
	snapshot, err := f.observer.Capture(t.Context(), bridgeobserve.ModeFull, "")
	if err != nil {
		t.Fatal(err)
	}
	refs := map[string]string{}
	for _, node := range snapshot.Nodes {
		if node.Ref != "" && refs[node.Name] == "" {
			refs[node.Name] = node.Ref
		}
	}
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindClick, Ref: refs["Counter"], Human: &HumanProfile{Seed: 11}}))
	click := readHumanCounts(t, f)
	if click.Moves < 5 || click.Downs != 1 || click.Ups != 1 {
		t.Fatalf("human click collapsed to a direct move/click: %+v", click)
	}
	text := strings.Repeat("artemis ", 5)
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindFill, Ref: refs["Name"], Text: text, Human: &HumanProfile{Seed: 7, KeyDelay: 15 * time.Millisecond, KeyVariance: 5 * time.Millisecond, TypoRate: 0.2}}))
	typed := readHumanCounts(t, f)
	if typed.Backspaces == 0 || typed.KeyDowns-click.KeyDowns <= len(text) || typed.KeyDowns != typed.KeyUps {
		t.Fatalf("human typing did not emit corrected keystrokes: %+v", typed)
	}
	assertValue(t, f.runtime, `document.querySelector('[aria-label="Name"]').value===`+jsString(t, text))
	requireAction(t, f.runtime.Execute(t.Context(), Request{Kind: KindScroll, DeltaY: 600, Human: &HumanProfile{ScrollSteps: 8, ScrollDuration: 160 * time.Millisecond}}))
	scrolled := readHumanCounts(t, f)
	if scrolled.Wheels-typed.Wheels < 6 {
		t.Fatalf("human scroll collapsed into one wheel event: %+v", scrolled)
	}
	cancelled, cancel := context.WithTimeout(t.Context(), 120*time.Millisecond)
	defer cancel()
	if out := f.runtime.Execute(cancelled, Request{Kind: KindType, Ref: refs["Name"], Text: strings.Repeat("x", 40), Human: &HumanProfile{KeyDelay: 40 * time.Millisecond}}); out.Success {
		t.Fatal("cancelled human typing reported success")
	}
	after := readHumanCounts(t, f)
	if after.KeyDowns != after.KeyUps || after.Downs != after.Ups {
		t.Fatalf("cancellation left a pressed key or button: %+v", after)
	}
}

func jsString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
