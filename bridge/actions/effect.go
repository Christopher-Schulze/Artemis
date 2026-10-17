package actions

import (
	"crypto/rand"
	"encoding/hex"
)

// EffectState describes execution, without inventing a business receipt.
type EffectState string

const (
	EffectNotAttempted EffectState = "not_attempted"
	EffectReadOnly     EffectState = "read_only"
	EffectPossible     EffectState = "may_have_occurred"
	EffectDispatched   EffectState = "dispatched"
)

func permitsActionReplay(kind Kind) bool {
	switch kind {
	case KindScreenshot, KindWait, KindTabList:
		return true
	default:
		return false
	}
}

func newActionID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}
