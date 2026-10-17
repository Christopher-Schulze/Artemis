package actions

import "testing"

func TestEveryActionHasRuntimeOwnedReplayAdmission(t *testing.T) {
	tests := []struct {
		kind   Kind
		replay bool
	}{
		{KindNavigate, false}, {KindReload, false}, {KindBack, false}, {KindForward, false},
		{KindWait, true}, {KindFocus, false}, {KindClick, false}, {KindHover, false},
		{KindScroll, false}, {KindKey, false}, {KindType, false}, {KindClear, false},
		{KindFill, false}, {KindFillForm, false}, {KindSelect, false}, {KindCheck, false},
		{KindUncheck, false}, {KindDrag, false}, {KindUpload, false}, {KindDownload, false},
		{KindScreenshot, true}, {KindPDF, false}, {KindDialog, false}, {KindTabOpen, false},
		{KindTabClose, false}, {KindTabList, true}, {KindTabSwitch, false}, {KindEvaluate, false},
		{KindAssert, false}, {KindViewport, false}, {KindFrameEvaluate, false}, {Kind("unknown"), false},
	}
	for _, test := range tests {
		t.Run(string(test.kind), func(t *testing.T) {
			if got := permitsActionReplay(test.kind); got != test.replay {
				t.Fatalf("replay admission=%v want=%v", got, test.replay)
			}
		})
	}
}
