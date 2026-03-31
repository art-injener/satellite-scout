package tracker

import "strings"

// ParseSatName разбивает полное имя спутника из TLE на основное и альтернативное.
//
//	"ISS (ZARYA)"          → "ISS",       "ZARYA"
//	"YUBILEINY (RS30)"     → "YUBILEINY", "RS30"
//	"RS-44 & BREEZE-KM R/B" → "RS-44 & BREEZE-KM R/B", ""
//	"METEOR-M 2-3"         → "METEOR-M 2-3", ""
func ParseSatName(fullName string) (primary, alias string) {
	idx := strings.Index(fullName, "(")
	if idx < 0 {
		return strings.TrimSpace(fullName), ""
	}
	primary = strings.TrimSpace(fullName[:idx])
	rest := fullName[idx+1:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return strings.TrimSpace(fullName), ""
	}
	alias = strings.TrimSpace(rest[:end])
	return primary, alias
}
