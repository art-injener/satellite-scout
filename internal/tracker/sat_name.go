package tracker

import "strings"

// ParseSatName разбивает полное имя спутника из TLE на основное и альтернативное.
//
//	"ISS (ZARYA)"            → "ISS",       "ZARYA"
//	"YUBILEINY (RS30)"       → "YUBILEINY", "RS30"
//	"RS-44 & BREEZE-KM R/B"  → "RS-44",     "BREEZE-KM R/B"
//	"METEOR-M 2-3"           → "METEOR-M 2-3", ""
func ParseSatName(fullName string) (string, string) {
	if left, right, ok := strings.Cut(fullName, "("); ok {
		if alias, _, found := strings.Cut(right, ")"); found {
			return strings.TrimSpace(left), strings.TrimSpace(alias)
		}
	}
	if left, right, ok := strings.Cut(fullName, " & "); ok {
		return strings.TrimSpace(left), strings.TrimSpace(right)
	}
	return strings.TrimSpace(fullName), ""
}
