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
	events   chan SSEEvent // Канал событий для этого клиента.
	clientID string        // Идентификатор клиента (из query ?client_id=).
}

// clientMessage — направленное сообщение конкретному клиенту.
type clientMessage struct {
	clientID string
	event    SSEEvent
}

// SSEHub управляет подключениями SSE-клиентов и рассылкой событий.
// Все данные (позиции, треки, зона видимости) доставляются через Hub.
// Владеет картой клиентов — все мутации происходят в горутине Run.
// Кеширует последние события track и position для мгновенной отправки новым клиентам.
type SSEHub struct {
	register   chan *sseClient    // Канал регистрации новых клиентов.
	unregister chan *sseClient    // Канал отписки клиентов.
	broadcast  chan SSEEvent      // Канал для рассылки событий всем клиентам.
	directed   chan clientMessage // Канал для отправки события конкретному клиенту.

	// notifyOnConnect — при регистрации клиента сюда отправляется сигнал, об отправки состояния спутников
	notifyOnConnect chan struct{}

	// onClientConnect — колбэк при подключении нового клиента.
	// Вызывается в горутине Run с clientID. Возвращает events для per-client отправки.
	onClientConnect func(clientID string) []SSEEvent

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
		directed:   make(chan clientMessage, 64),
		done:       make(chan struct{}),
	}
}

// Run запускает основной цикл обработки SSE Hub.
// Блокирует выполнение до отмены контекста.
// Владеет картой клиентов — все операции с ней происходят в одной горутине.
// Кеширует последние position и track события для мгновенной отправки новым клиентам.
func (h *SSEHub) Run(ctx context.Context) {
	st := &hubState{
		hub:         h,
		clients:     make(map[*sseClient]bool),
		clientIDMap: make(map[string]*sseClient),
		lastEvents:  make(map[string]SSEEvent),
	}

	defer st.cleanup(ctx)

	slog.InfoContext(ctx, "SSE hub started")

	for {
		select {
		case client := <-h.register:
			st.registerClient(ctx, client)
		case client := <-h.unregister:
			st.unregisterClient(ctx, client)
		case event := <-h.broadcast:
			st.broadcastEvent(ctx, event)
		case msg := <-h.directed:
			st.sendDirected(ctx, msg)
		case <-ctx.Done():
			return
		}
	}
}

// hubState хранит локальное состояние event-loop SSEHub.Run.
type hubState struct {
	hub         *SSEHub
	clients     map[*sseClient]bool
	clientIDMap map[string]*sseClient
	lastEvents  map[string]SSEEvent
}

func (st *hubState) registerClient(ctx context.Context, client *sseClient) {
	st.clients[client] = true
	if client.clientID != "" {
		st.clientIDMap[client.clientID] = client
	}
	st.hub.clientCount.Add(1)
	slog.DebugContext(ctx, "SSE client registered",
		"client_id", client.clientID,
		"total_clients", st.hub.clientCount.Load(),
	)

	sendCachedEvents(client, st.lastEvents)

	if st.hub.onClientConnect != nil && client.clientID != "" {
		for _, evt := range st.hub.onClientConnect(client.clientID) {
			select {
			case client.events <- evt:
			default:
			}
		}
	}

	if st.hub.notifyOnConnect != nil {
		select {
		case st.hub.notifyOnConnect <- struct{}{}:
		default:
		}
	}
}

func (st *hubState) unregisterClient(ctx context.Context, client *sseClient) {
	if _, exists := st.clients[client]; !exists {
		return
	}
	close(client.events)
	delete(st.clients, client)
	if client.clientID != "" {
		delete(st.clientIDMap, client.clientID)
	}
	st.hub.clientCount.Add(-1)
	slog.DebugContext(ctx, "SSE client unregistered",
		"client_id", client.clientID,
		"total_clients", st.hub.clientCount.Load(),
	)
}

func (st *hubState) broadcastEvent(ctx context.Context, event SSEEvent) {
	st.lastEvents[event.Type] = event
	broadcastToClients(ctx, event, st.clients)
}

func (st *hubState) sendDirected(ctx context.Context, msg clientMessage) {
	client, ok := st.clientIDMap[msg.clientID]
	if !ok {
		return
	}
	select {
	case client.events <- msg.event:
	default:
		slog.WarnContext(ctx, "SSE directed: client buffer full",
			"client_id", msg.clientID, "event_type", msg.event.Type)
	}
}

func (st *hubState) cleanup(ctx context.Context) {
	st.hub.once.Do(func() { close(st.hub.done) })
	for client := range st.clients {
		close(client.events)
	}
	st.hub.clientCount.Store(0)
	slog.InfoContext(ctx, "SSE hub stopped", "cleaned_clients", len(st.clients))
}

// sendCachedEvents отправляет кешированные события новому клиенту.
// Порядок важен:
//  1. satellite_group_update — состав группы, primary, tracking_id (источник истины для новых клиентов)
//  2. satellite_state_update — последние позиции/треки
//
// satellite_change НЕ кешируется: это событие-транзакция («что произошло»),
// актуально только для живых клиентов. Отправлять его новому клиенту бессмысленно
// и вызывает мелькание индикатора наблюдения при обновлении страницы.
func sendCachedEvents(client *sseClient, lastEvents map[string]SSEEvent) {
	for _, eventType := range []string{
		"satellite_group_update",
		"satellite_state_update",
	} {
		if cached, ok := lastEvents[eventType]; ok {
			select {
			case client.events <- cached:
			default:
			}
		}
	}
}

// broadcastToClients рассылает событие всем подключённым клиентам.
func broadcastToClients(ctx context.Context, event SSEEvent, clients map[*sseClient]bool) {
	for client := range clients {
		select {
		case client.events <- event:
		default:
			slog.WarnContext(ctx, "SSE client buffer full, dropping event", "event_type", event.Type)
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

// SendToClient отправляет событие конкретному клиенту по clientID.
func (h *SSEHub) SendToClient(clientID string, eventType string, data []byte) {
	msg := clientMessage{
		clientID: clientID,
		event:    SSEEvent{Type: eventType, Data: data},
	}
	select {
	case h.directed <- msg:
	case <-h.done:
	}
}

// SetOnClientConnect задаёт колбэк для per-client событий при подключении.
// Колбэк вызывается в горутине Run. Вызывать до Run().
func (h *SSEHub) SetOnClientConnect(fn func(clientID string) []SSEEvent) {
	h.onClientConnect = fn
}

// SetNotifyOnConnect задаёт канал, в который отправляется сигнал при регистрации нового клиента.
// Буфер 1, чтобы не блокировать Hub. Вызывать до Run().
func (h *SSEHub) SetNotifyOnConnect(ch chan struct{}) {
	h.notifyOnConnect = ch
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

	clientID := r.URL.Query().Get("client_id")

	client := &sseClient{
		events:   make(chan SSEEvent, clientEventBufferSize),
		clientID: clientID,
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
