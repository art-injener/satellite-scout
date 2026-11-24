// Package payload — разбор полезной нагрузки кадра (ADR-009 §7).
package payload

import "encoding/hex"

// Вид полезной нагрузки для колонки «Тип» и отрисовки строки.
const (
	KindBeacon  = "beacon"
	KindText    = "text"
	KindImage   = "image"
	KindFile    = "file"
	KindPos     = "position"
	KindSonde   = "sonde" // метеозонд (RS41 и др.)
	KindLine    = "line"  // сырая пачка линии без кадрового протокола (OOK / FSK эфир)
	KindVoice   = "voice" // аналоговый голос / CW (нет кадра, только звук)
	KindUnknown = "unknown"
)

// Frame — сырой кадр после CRC и контекст приёма.
type Frame struct {
	Raw         []byte
	Profile     string
	NORAD       int
	FrequencyHz uint64
	TimestampMs uint64
}

// Param — поле маяка или разобранного кадра.
type Param struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Key   bool   `json:"key,omitempty"`
}

// Payload — конверт полезной нагрузки после разбора.
type Payload struct {
	Kind    string
	Decoder string
	Summary string
	Src     string
	Dest    string
	Info    string
	InfoHex string
	Params  []Param
	// Кусок снимка (kind=image); сборщик — отдельная задача.
	Offset int
	Chunk  []byte
}

// Decoder подтверждает формат кадра и разбирает его; ok=false — формат не подтверждён.
type Decoder interface {
	ID() string
	Decode(f Frame) (Payload, bool)
}

// Label — русская подпись kind для колонки «Тип».
func Label(kind string) string {
	switch kind {
	case KindBeacon:
		return "Маяк ТМИ"
	case KindText:
		return "Сообщение"
	case KindImage:
		return "Снимок"
	case KindFile:
		return "Файл"
	case KindPos:
		return "Навигация"
	case KindSonde:
		return "Зонд"
	case KindLine:
		return "Линия"
	case KindVoice:
		return "Голос"
	default:
		return "Неизвестно"
	}
}

// Unknown — конверт без подтверждённого формата: только hex.
func Unknown(raw []byte) Payload {
	return Payload{
		Kind:    KindUnknown,
		Decoder: "unknown",
		Summary: Label(KindUnknown),
		InfoHex: hex.EncodeToString(raw),
	}
}

// Line — сырая пачка бит линии (OOK / FSK без CRC): hex в модалку «Кадр».
func Line(raw []byte) Payload {
	return Payload{
		Kind:    KindLine,
		Decoder: "line",
		Summary: Label(KindLine),
		InfoHex: hex.EncodeToString(raw),
	}
}
