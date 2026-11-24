package bandscan

import (
	"math"
	"slices"
)

// hearJoinHz — кадр/голос без попадания в полосу участка клеится, если ближе этого.
const hearJoinHz = 50_000

// hearHistoryMax — сколько последних приёмов держать на участке до рестарта (модалка «Кадр»).
const hearHistoryMax = 16

// hearHistoryHexMax — обрезка hex в истории, чтобы SSE band_signals не раздувался.
const hearHistoryHexMax = 512

// HearParam — поле последнего кадра для модалки «Кадр» (как telemetry.params).
type HearParam struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Key   bool   `json:"key,omitempty"`
}

// HearRx — одна запись журнала приёма участка (без служебных полей HearNote).
type HearRx struct {
	Kind        string      `json:"kind,omitempty"`
	Summary     string      `json:"summary,omitempty"`
	Decoder     string      `json:"decoder,omitempty"`
	Src         string      `json:"src,omitempty"`
	Dest        string      `json:"dest,omitempty"`
	Info        string      `json:"info,omitempty"`
	InfoHex     string      `json:"info_hex,omitempty"`
	Params      []HearParam `json:"params,omitempty"`
	TimestampMs uint64      `json:"timestamp_ms,omitempty"`
}

// HearNote — принятые данные для строки «Эфир» (кадр или голос).
type HearNote struct {
	Kind        string
	Summary     string
	Decoder     string
	Src         string
	Dest        string
	Info        string
	InfoHex     string
	Params      []HearParam
	TimestampMs uint64
	NoradID     int
	SatName     string
	// CountFrame — увеличить frames_good (цифровой кадр); голос — false.
	CountFrame bool
}

// RecordRx привязывает приём к ближайшему участку по частоте.
// Сначала попадание в полосу, иначе ≤ hearJoinHz; иначе тот же NORAD; иначе новый участок.
func (t *Tracker) RecordRx(freqHz float64, note HearNote) bool {
	s := t.matchHear(freqHz)
	if s == nil && note.NoradID > 0 {
		s = t.matchHearNorad(note.NoradID)
	}
	if s == nil {
		if !(freqHz > 0) {
			return false
		}
		ms := note.TimestampMs
		if ms == 0 {
			ms = t.nowMs
		}
		if ms == 0 {
			ms = 1
		}
		s = t.addHearSegment(freqHz, ms)
	}
	if note.CountFrame {
		s.framesGood++
	}
	if note.Kind != "" {
		s.lastKind = note.Kind
	}
	if note.Summary != "" {
		s.lastSummary = note.Summary
	}
	if note.Decoder != "" {
		s.lastDecoder = note.Decoder
	}
	// Тело последнего кадра — для модалки «Кадр» без отдельного SSE telemetry на участке.
	if note.Kind != "" || note.InfoHex != "" || note.Info != "" || len(note.Params) > 0 {
		s.lastSrc = note.Src
		s.lastDest = note.Dest
		s.lastInfo = note.Info
		s.lastInfoHex = note.InfoHex
		s.lastParams = cloneHearParams(note.Params)
		ms := note.TimestampMs
		if ms == 0 {
			ms = t.nowMs
		}
		s.pushHearRx(noteToHearRx(note, ms))
	}
	if note.TimestampMs > 0 {
		s.lastRxMs = note.TimestampMs
	} else if s.lastRxMs == 0 && t.nowMs > 0 {
		s.lastRxMs = t.nowMs
	}
	if note.NoradID > 0 {
		s.stickyNorad = note.NoradID
		if note.SatName != "" {
			s.stickyName = note.SatName
		}
		if s.stickyOrigin == "" || s.stickyOrigin == OriginUnknown {
			s.stickyOrigin = OriginSatellite
		}
	}
	s.shown = true
	return true
}

func (t *Tracker) matchHear(freqHz float64) *segment {
	if !(freqHz > 0) || len(t.segs) == 0 {
		return nil
	}
	var inBand *segment
	inDist := math.Inf(1)
	var near *segment
	nearDist := math.Inf(1)
	for _, s := range t.segs {
		c := s.center()
		if c == 0 {
			c = s.centerHz
		}
		half := (s.hiHz - s.loHz) / 2
		d := math.Abs(c - freqHz)
		if half > 0 && d <= half && d < inDist {
			inBand, inDist = s, d
		}
		if d < nearDist {
			near, nearDist = s, d
		}
	}
	if inBand != nil {
		return inBand
	}
	if near != nil && nearDist <= hearJoinHz {
		return near
	}
	return nil
}

func (t *Tracker) matchHearNorad(norad int) *segment {
	if norad <= 0 {
		return nil
	}
	var best *segment
	for _, s := range t.segs {
		if s.stickyNorad != norad {
			continue
		}
		if best == nil || s.lastSeenMs >= best.lastSeenMs {
			best = s
		}
	}
	return best
}

// addHearSegment — участок только от приёма, когда детектор ещё не завёл строку.
func (t *Tracker) addHearSegment(freqHz float64, ms uint64) *segment {
	s := t.add(ms)
	half := 6_000.0
	s.centerHz = freqHz
	s.loHz = freqHz - half
	s.hiHz = freqHz + half
	s.refHz = freqHz
	s.hits = confirmHits
	s.onMs = float64(rememberMinOnMs)
	s.observedMs = float64(rememberMinOnMs)
	s.lastSeenMs = ms
	if t.nowMs < ms {
		t.nowMs = ms
	}
	return s
}

// pinned — строка КА с реальным приёмом: не выкидывать по таймеру памяти.
func (s *segment) pinned() bool {
	return s.stickyNorad > 0 && (s.framesGood > 0 || s.lastKind != "")
}

// absorbHear переносит накопленный приём с уходящего участка на оставшийся.
func (keep *segment) absorbHear(s *segment) {
	keep.framesGood += s.framesGood
	if s.lastRxMs >= keep.lastRxMs && s.lastKind != "" {
		keep.lastKind = s.lastKind
		keep.lastSummary = s.lastSummary
		keep.lastDecoder = s.lastDecoder
		keep.lastSrc = s.lastSrc
		keep.lastDest = s.lastDest
		keep.lastInfo = s.lastInfo
		keep.lastInfoHex = s.lastInfoHex
		keep.lastParams = cloneHearParams(s.lastParams)
		keep.lastRxMs = s.lastRxMs
	} else if keep.lastKind == "" {
		keep.lastKind = s.lastKind
		keep.lastSummary = s.lastSummary
		keep.lastDecoder = s.lastDecoder
		keep.lastSrc = s.lastSrc
		keep.lastDest = s.lastDest
		keep.lastInfo = s.lastInfo
		keep.lastInfoHex = s.lastInfoHex
		keep.lastParams = cloneHearParams(s.lastParams)
		keep.lastRxMs = s.lastRxMs
	}
	keep.rxHistory = mergeHearHistory(keep.rxHistory, s.rxHistory)
}

func (s *segment) pushHearRx(rx HearRx) {
	if rx.Kind == "" && rx.InfoHex == "" && rx.Info == "" && len(rx.Params) == 0 {
		return
	}
	s.rxHistory = append(s.rxHistory, rx)
	if len(s.rxHistory) > hearHistoryMax {
		s.rxHistory = append([]HearRx(nil), s.rxHistory[len(s.rxHistory)-hearHistoryMax:]...)
	}
}

func noteToHearRx(note HearNote, ms uint64) HearRx {
	hex := note.InfoHex
	if len(hex) > hearHistoryHexMax {
		hex = hex[:hearHistoryHexMax]
	}
	return HearRx{
		Kind:        note.Kind,
		Summary:     note.Summary,
		Decoder:     note.Decoder,
		Src:         note.Src,
		Dest:        note.Dest,
		Info:        note.Info,
		InfoHex:     hex,
		Params:      cloneHearParams(note.Params),
		TimestampMs: ms,
	}
}

// mergeHearHistory склеивает журналы при слиянии участков: по времени, хвост не длиннее лимита.
func mergeHearHistory(a, b []HearRx) []HearRx {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	out := make([]HearRx, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	slices.SortStableFunc(out, func(x, y HearRx) int {
		switch {
		case x.TimestampMs < y.TimestampMs:
			return -1
		case x.TimestampMs > y.TimestampMs:
			return 1
		default:
			return 0
		}
	})
	if len(out) > hearHistoryMax {
		out = append([]HearRx(nil), out[len(out)-hearHistoryMax:]...)
	}
	return out
}

func cloneHearParams(in []HearParam) []HearParam {
	if len(in) == 0 {
		return nil
	}
	out := make([]HearParam, len(in))
	copy(out, in)
	return out
}

func cloneHearHistory(in []HearRx) []HearRx {
	if len(in) == 0 {
		return nil
	}
	out := make([]HearRx, len(in))
	for i := range in {
		out[i] = in[i]
		out[i].Params = cloneHearParams(in[i].Params)
	}
	return out
}
