package stealth

// PatchCount returns the number of stealth patches embedded in Script().
// Per spec ss28.6.1.1: 27 zero-cost patches in StealthStealth,
// +2 paranoid-only patches in StealthParanoid (total 29).
func PatchCount() int {
	return BasePatchCount
}
