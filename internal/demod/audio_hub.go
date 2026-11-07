package demod

import (
	"math"
	"sync"
)

// audioListenerBuffer — сколько блоков звука ждёт медленного слушателя, дальше новые
// выбрасываются: блок IQ длится 7–16 мс, так что это около секунды.
const audioListenerBuffer = 128

// AudioBlock — звук одного блока IQ тракта: PCM с частотой AudioRate, полная шкала ±32767.
// Срез общий у всех слушателей и не меняется.
type AudioBlock struct {
	TimestampMs uint64 // начало блока IQ
	FrequencyHz uint64 // частота канала: настройка приёмника плюс сдвиг
	Samples     []int16
}

// AudioHub раздаёт звук трактов слушателям. Медленный слушатель теряет блоки,
// не задерживая демодулятор. Потокобезопасен.
type AudioHub struct {
	mu   sync.Mutex
	subs map[string]map[chan AudioBlock]struct{}
}

// NewAudioHub создаёт пустую раздачу.
func NewAudioHub() *AudioHub {
	return &AudioHub{subs: make(map[string]map[chan AudioBlock]struct{})}
}

// Subscribe подключает слушателя тракта и возвращает канал блоков и функцию отписки,
// которая закрывает канал; её можно звать повторно.
func (h *AudioHub) Subscribe(pathID string) (<-chan AudioBlock, func()) {
	ch := make(chan AudioBlock, audioListenerBuffer)
	h.mu.Lock()
	if h.subs[pathID] == nil {
		h.subs[pathID] = make(map[chan AudioBlock]struct{})
	}
	h.subs[pathID][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[pathID], ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// Publish отдаёт звук блока слушателям тракта; без слушателей ничего не делает.
// audio — в долях полной шкалы, громче ±1 обрезается.
func (h *AudioHub) Publish(pathID string, timestampMs, frequencyHz uint64, audio []float32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subs := h.subs[pathID]
	if len(subs) == 0 {
		return
	}
	pcm := make([]int16, len(audio))
	for i, v := range audio {
		pcm[i] = int16(math.Round(math.Max(-1, math.Min(1, float64(v))) * math.MaxInt16))
	}
	b := AudioBlock{TimestampMs: timestampMs, FrequencyHz: frequencyHz, Samples: pcm}
	for ch := range subs {
		select {
		case ch <- b:
		default:
		}
	}
}
