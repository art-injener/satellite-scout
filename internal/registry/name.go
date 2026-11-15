package registry

import "strings"

// spaceTrackTBA — имя, которое Space-Track ставит КА до опознания.
const spaceTrackTBA = "TBA - TO BE ASSIGNED"

// DisplayName заменяет заглушку Space-Track в имени TLE («OBJECT C», «TBA - TO BE ASSIGNED»,
// пусто) именем из curated или sonik. Настоящее имя TLE не трогает: в нём есть другие имена
// в скобках, которые разбирают пролёты.
func (r *Registry) DisplayName(norad int, tleName string) string {
	if !placeholderName(tleName) {
		return tleName
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, tx := range r.txs[norad][SourceCurated] {
		if tx.Name != "" {
			return tx.Name
		}
	}
	if f, ok := r.facts[norad][SourceSonik]; ok && f.Name != "" && !placeholderName(f.Name) {
		return f.Name
	}
	return tleName
}

func placeholderName(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || name == spaceTrackTBA || strings.HasPrefix(name, "OBJECT ")
}
