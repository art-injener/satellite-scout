package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
)

// Размер буфера канала событий клиента.
// 64 события — запас на ~64 секунд при 1 event/sec (position).
const clientEventBufferSize = 64

// Размер буфера канала рассылки Hub.
const hubBroadcastBufferSize = 256

// SSEEvent — одно SSE-событие для отправки клиентам.
type SSEEvent struct {
	Type string // Тип события (position, track, satellite_change и т.д.).
	Data []byte // JSON-данные события.
}

// sseClient — подключённый SSE-клиент.
type sseClient struct {
	events chan SSEEvent // Канал событий для этого клиента.
}

// SSEHub управляет подключениями SSE-клиентов и рассылкой событий.
// Все данные (позиции, треки, зона видимости) доставляются через Hub.
// Владеет картой клиентов — все мутации происходят в горутине Run.
// Кеширует последние события track и position для мгновенной отправки новым клиентам.
type SSEHub struct {
	register   chan *sseClient // Канал регистрации новых клиентов.
	unregister chan *sseClient // Канал отписки клиентов.
	broadcast  chan SSEEvent   // Канал для рассылки событий всем клиентам.

	clientCount atomic.Int64 // Атомарный счётчик подключённых клиентов.

	done chan struct{} // Закрывается при завершении Run.
	once sync.Once     // Гарантия однократного закрытия done.
}

// NewSSEHub создаёт новый SSE Hub.
func NewSSEHub() *SSEHub {
	return &SSEHub{
		register:   make(chan *sseClient, 16),
		unregister: make(chan *sseClient, 16),
		broadcast:  make(chan SSEEvent, hubBroadcastBufferSize),
		done:       make(chan struct{}),
	}
}

// Run запускает основной цикл обработки SSE Hub.
// Блокирует выполнение до отмены контекста.
// Владеет картой клиентов — все операции с ней происходят в одной горутине.
// Кеширует последние position и track события для мгновенной отправки новым клиентам.
func (h *SSEHub) Run(ctx context.Context) {
	clients := make(map[*sseClient]bool)

	// Кеш последних событий по типу для отправки новым клиентам.
	lastEvents := make(map[string]SSEEvent)

	defer func() {
		// Сигнализируем о завершении Hub (до закрытия каналов клиентов,
		// чтобы ServeHTTP не пытался писать в закрытый канал unregister).
		h.once.Do(func() { close(h.done) })

		// Закрываем каналы всех оставшихся клиентов.
		for client := range clients {
			close(client.events)
		}

		h.clientCount.Store(0)
		slog.InfoContext(ctx, "SSE hub stopped", "cleaned_clients", len(clients))
	}()

	slog.InfoContext(ctx, "SSE hub started")

	for {
		select {
		case client := <-h.register:
			clients[client] = true
			h.clientCount.Add(1)
			slog.DebugContext(ctx, "SSE client registered", "total_clients", h.clientCount.Load())

			// Отправка кешированных событий новому клиенту.
			for _, eventType := range []string{"satellite_state_update", "satellite_change"} {
				if cached, ok := lastEvents[eventType]; ok {
					select {
					case client.events <- cached:
					default:
					}
				}
			}

		case client := <-h.unregister:
			if _, exists := clients[client]; exists {
				close(client.events)
				delete(clients, client)
				h.clientCount.Add(-1)
				slog.DebugContext(ctx, "SSE client unregistered", "total_clients", h.clientCount.Load())
			}

		case event := <-h.broadcast:
			// Кешируем последние события по типу.
			lastEvents[event.Type] = event

			for client := range clients {
				select {
				case client.events <- event:
					// Событие отправлено.
				default:
					// Буфер клиента полон — пропускаем (защита от медленных клиентов).
					slog.WarnContext(ctx, "SSE client buffer full, dropping event", "event_type", event.Type)
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// Broadcast отправляет событие всем подключённым клиентам.
// Если Hub остановлен, событие игнорируется (без блокировки).
func (h *SSEHub) Broadcast(eventType string, data []byte) {
	event := SSEEvent{Type: eventType, Data: data}

	select {
	case h.broadcast <- event:
	case <-h.done:
		// Hub уже остановлен — событие игнорируется.
	}
}

// ClientCount возвращает количество подключённых клиентов.
func (h *SSEHub) ClientCount() int {
	return int(h.clientCount.Load())
}

// Done возвращает канал, который закрывается при остановке Hub.
// Полезен для ожидания полной остановки Hub извне.
func (h *SSEHub) Done() <-chan struct{} {
	return h.done
}

// ServeHTTP обрабатывает SSE-подключение клиента.
// Реализует интерфейс http.Handler. Совместим с EventSource API браузера.
func (h *SSEHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Проверяем поддержку streaming.
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// SSE заголовки.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Отключаем буферизацию nginx.

	// Создаём клиента.
	client := &sseClient{
		events: make(chan SSEEvent, clientEventBufferSize),
	}

	// Регистрация (неблокирующая — Hub может быть уже остановлен).
	select {
	case h.register <- client:
	case <-h.done:
		return
	}

	// Отписка при отключении клиента.
	defer func() {
		select {
		case h.unregister <- client:
		case <-h.done:
			// Hub уже остановлен — клиенты очищены в Run.
		}
	}()

	// Приветственное событие.
	if _, err := fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"ok\"}\n\n"); err != nil {
		slog.Debug("failed to write SSE connected event", "error", err)
		return
	}

	flusher.Flush()

	// Основной цикл отправки событий.
	for {
		select {
		case event, open := <-client.events:
			if !open {
				// Канал закрыт — Hub остановлен или клиент удалён.
				return
			}

			// Формат SSE: event: <type>\ndata: <json>\n\n
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, event.Data); err != nil {
				slog.Debug("failed to write SSE event", "event_type", event.Type, "error", err)
				return
			}

			flusher.Flush()

		case <-r.Context().Done():
			// HTTP-клиент отключился.
			return

		case <-h.done:
			// Hub остановлен.
			return
		}
	}
}
