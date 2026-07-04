package stealth

import "testing"

func TestPatchCountMatchesSpec(t *testing.T) {
	// Spec ss28.6.1.1: 27 zero-cost patches in StealthStealth,
	// +2 paranoid-only patches in StealthParanoid (total 29).
	got := PatchCount()
	if got != 27 {
		t.Errorf("PatchCount() = %d, want 27 (spec ss28.6.1.1)", got)
	}
}

func TestPatchCountForStealth(t *testing.T) {
	got := PatchCountFor(StealthStealth)
	if got != 27 {
		t.Errorf("PatchCountFor(StealthStealth) = %d, want 27", got)
	}
}

func TestPatchCountForParanoid(t *testing.T) {
	got := PatchCountFor(StealthParanoid)
	if got != 29 {
		t.Errorf("PatchCountFor(StealthParanoid) = %d, want 29", got)
	}
}

func TestPatchCountForDefault(t *testing.T) {
	got := PatchCountFor(StealthDefault)
	if got != 0 {
		t.Errorf("PatchCountFor(StealthDefault) = %d, want 0", got)
	}
}

func TestBasePatchCountConstant(t *testing.T) {
	if BasePatchCount != 27 {
		t.Errorf("BasePatchCount = %d, want 27", BasePatchCount)
	}
}

func TestParanoidPatchCountConstant(t *testing.T) {
	if ParanoidPatchCount != 2 {
		t.Errorf("ParanoidPatchCount = %d, want 2", ParanoidPatchCount)
	}
}
