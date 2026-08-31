package comb

import (
	"fmt"
	"strings"
)

// signOrder is the canonical rendering order of the finding classes.
const signOrder = "DUABSELO"

// SignSet selects which finding classes a run looks for and reports.
// The zero value selects every class. Classes outside the set are
// neither probed nor rendered, and the exit status follows what was
// asked for; probe failures are always reported.
type SignSet struct {
	set string // canonical-order subset; empty means all
}

// ParseSignSet reads a sign list such as "DUS", "dus", or "D,U S".
func ParseSignSet(s string) (SignSet, error) {
	upper := strings.ToUpper(s)
	for _, r := range upper {
		if r == ',' || r == ' ' {
			continue
		}
		if !strings.ContainsRune(signOrder, r) {
			return SignSet{}, fmt.Errorf("unknown sign %q (valid signs: %s)", string(r), signOrder)
		}
	}
	var b strings.Builder
	for _, r := range signOrder {
		if strings.ContainsRune(upper, r) {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return SignSet{}, fmt.Errorf("empty sign list (valid signs: %s)", signOrder)
	}
	if b.Len() == len(signOrder) {
		return SignSet{}, nil // the full set is the same as the zero value
	}
	return SignSet{set: b.String()}, nil
}

// All reports whether every class is selected.
func (s SignSet) All() bool { return s.set == "" }

// Complement returns the classes not in s — how --except turns "hide
// these" into a selection. Complementing the full set would select
// nothing, which cannot be expressed and cannot be meant.
func (s SignSet) Complement() (SignSet, error) {
	if s.All() {
		return SignSet{}, fmt.Errorf("excluding every sign would leave nothing to look for")
	}
	var b strings.Builder
	for _, r := range signOrder {
		if !strings.ContainsRune(s.set, r) {
			b.WriteRune(r)
		}
	}
	return SignSet{set: b.String()}, nil
}

// Has reports whether the class is selected.
func (s SignSet) Has(sign byte) bool {
	return s.set == "" || strings.IndexByte(s.set, sign) >= 0
}

// Filter keeps only the selected classes of a rendered sign string.
func (s SignSet) Filter(signs string) string {
	if s.set == "" {
		return signs
	}
	var b strings.Builder
	for i := 0; i < len(signs); i++ {
		if s.Has(signs[i]) {
			b.WriteByte(signs[i])
		}
	}
	return b.String()
}

// String renders the selection in canonical order.
func (s SignSet) String() string {
	if s.set == "" {
		return signOrder
	}
	return s.set
}
