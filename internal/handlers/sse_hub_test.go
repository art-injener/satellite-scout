package handlers

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Вспомогательные функции для тестов ---

// startTestHub запускает Hub и возвращает его с функцией отмены.
func startTestHub(t *testing.T) (*SSEHub, context.CancelFunc) {
	t.Helper()

	hub := NewSSEHub()
	ctx, cancel := context.WithCancel(context.Background())

	go hub.Run(ctx)

	return hub, cancel
}

// waitForClientCount ожидает достижения заданного количества клиентов.
func waitForClientCount(t *testing.T, hub *SSEHub, expected int) {
	t.Helper()

	deadline := time.After(2 * time.Second)

	for hub.ClientCount() != expected {
		select {
		case <-deadline:
			t.Fatalf("expected %d clients, got %d", expected, hub.ClientCount())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// registerTestClient регистрирует тестового клиента напрямую в Hub.
func registerTestClient(hub *SSEHub) *sseClient {
	client := &sseClient{
		events: make(chan SSEEvent, clientEventBufferSize),
	}

	hub.register <- client

	return client
}

// readSSEEvent читает одно SSE-событие из Scanner.
// Формат: "event: <type>\ndata: <json>\n\n".
func readSSEEvent(t *testing.T, scanner *bufio.Scanner) (eventType, data string) {
	t.Helper()

	// Строка "event: <type>".
	if !scanner.Scan() {
		t.Fatalf("failed to read event line: %v", scanner.Err())
	}

	eventLine := scanner.Text()
	if !strings.HasPrefix(eventLine, "event: ") {
		t.Fatalf("expected 'event: ...' line, got: %q", eventLine)
	}

	eventType = strings.TrimPrefix(eventLine, "event: ")

	// Строка "data: <json>".
	if !scanner.Scan() {
		t.Fatalf("failed to read data line: %v", scanner.Err())
	}

	dataLine := scanner.Text()
	if !strings.HasPrefix(dataLine, "data: ") {
		t.Fatalf("expected 'data: ...' line, got: %q", dataLine)
	}

	data = strings.TrimPrefix(dataLine, "data: ")

	// Пустая строка (разделитель SSE).
	if !scanner.Scan() {
		t.Fatalf("failed to read SSE delimiter: %v", scanner.Err())
	}

	if scanner.Text() != "" {
		t.Fatalf("expected empty line delimiter, got: %q", scanner.Text())
	}

	return eventType, data
}

// --- Тесты ---

func TestNewSSEHub(t *testing.T) {
	hub := NewSSEHub()

	if hub == nil {
		t.Fatal("NewSSEHub returned nil")
	}

	if hub.ClientCount() != 0 {
		t.Errorf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestSSEHub_RunAndShutdown(t *testing.T) {
	hub := NewSSEHub()
	ctx, cancel := context.WithCancel(context.Background())

	runDone := make(chan struct{})

	go func() {
		hub.Run(ctx)
		close(runDone)
	}()

	// Отменяем контекст — Run должен завершиться.
	cancel()

	select {
	case <-runDone:
		// Run завершился корректно.
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}

	// Done-канал должен быть закрыт.
	select {
	case <-hub.Done():
		// OK.
	default:
		t.Fatal("Done channel should be closed after Run exits")
	}
}

func TestSSEHub_RegisterUnregister(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	client := registerTestClient(hub)
	waitForClientCount(t, hub, 1)

	// Отписка.
	hub.unregister <- client
	waitForClientCount(t, hub, 0)
}

func TestSSEHub_BroadcastSingleClient(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	client := registerTestClient(hub)
	waitForClientCount(t, hub, 1)

	hub.Broadcast("position", []byte(`{"lat":47.3,"lon":39.8}`))

	select {
	case event := <-client.events:
		if event.Type != "position" {
			t.Errorf("expected event type 'position', got %q", event.Type)
		}

		expected := `{"lat":47.3,"lon":39.8}`
		if string(event.Data) != expected {
			t.Errorf("expected data %q, got %q", expected, string(event.Data))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcast event")
	}
}

func TestSSEHub_BroadcastMultipleClients(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	const numClients = 5
	clients := make([]*sseClient, numClients)

	for i := range clients {
		clients[i] = registerTestClient(hub)
	}

	waitForClientCount(t, hub, numClients)

	hub.Broadcast("track", []byte(`{"norad_id":25544}`))

	for i, client := range clients {
		select {
		case event := <-client.events:
			if event.Type != "track" {
				t.Errorf("client %d: expected event type 'track', got %q", i, event.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("client %d: timeout waiting for broadcast event", i)
		}
	}
}

func TestSSEHub_SlowClientDropsEvent(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	// Клиент с очень маленьким буфером (1 событие).
	slowClient := &sseClient{
		events: make(chan SSEEvent, 1),
	}
	fastClient := registerTestClient(hub)

	hub.register <- slowClient
	waitForClientCount(t, hub, 2)

	// Заполняем буфер медленного клиента.
	hub.Broadcast("test", []byte(`{"msg":"1"}`))
	time.Sleep(50 * time.Millisecond)

	// Следующие события пропускаются для медленного клиента,
	// но доставляются быстрому.
	hub.Broadcast("test", []byte(`{"msg":"2"}`))
	hub.Broadcast("test", []byte(`{"msg":"3"}`))

	// Быстрый клиент получает все 3 события.
	received := 0
	timeout := time.After(2 * time.Second)

	for received < 3 {
		select {
		case <-fastClient.events:
			received++
		case <-timeout:
			t.Fatalf("fast client received only %d/3 events", received)
		}
	}

	// Медленный клиент получил только 1 (буфер = 1).
	select {
	case <-slowClient.events:
		// OK — получил 1 событие.
	default:
		t.Fatal("slow client should have received at least 1 event")
	}
}

func TestSSEHub_BroadcastAfterShutdown(t *testing.T) {
	hub, cancel := startTestHub(t)

	// Останавливаем Hub.
	cancel()

	// Ждём завершения.
	select {
	case <-hub.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("hub did not stop")
	}

	// Broadcast после остановки не должен блокировать или паниковать.
	done := make(chan struct{})

	go func() {
		hub.Broadcast("position", []byte(`{"lat":0}`))
		close(done)
	}()

	select {
	case <-done:
		// OK — Broadcast завершился без блокировки.
	case <-time.After(2 * time.Second):
		t.Fatal("Broadcast blocked after hub shutdown")
	}
}

func TestSSEHub_ShutdownClosesClientChannels(t *testing.T) {
	hub, cancel := startTestHub(t)

	client := registerTestClient(hub)
	waitForClientCount(t, hub, 1)

	// Останавливаем Hub.
	cancel()

	// Канал клиента должен быть закрыт.
	select {
	case _, ok := <-client.events:
		if ok {
			t.Fatal("expected client channel to be closed")
		}
		// OK — канал закрыт.
	case <-time.After(2 * time.Second):
		t.Fatal("client channel was not closed after hub shutdown")
	}
}

func TestSSEHub_ServeHTTP_Format(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	server := httptest.NewServer(hub)
	defer server.Close()

	// Подключаемся как SSE-клиент.
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	// Проверяем SSE-заголовки.
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}

	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got %q", cc)
	}

	scanner := bufio.NewScanner(resp.Body)

	// Читаем приветственное событие.
	eventType, data := readSSEEvent(t, scanner)

	if eventType != "connected" {
		t.Errorf("expected event type 'connected', got %q", eventType)
	}

	if data != `{"status":"ok"}` {
		t.Errorf("expected connected data '{\"status\":\"ok\"}', got %q", data)
	}

	// Ждём регистрации клиента.
	waitForClientCount(t, hub, 1)

	// Отправляем событие через Hub.
	hub.Broadcast("position", []byte(`{"norad_id":25544,"lat":47.3}`))

	// Читаем событие position.
	eventType, data = readSSEEvent(t, scanner)

	if eventType != "position" {
		t.Errorf("expected event type 'position', got %q", eventType)
	}

	if data != `{"norad_id":25544,"lat":47.3}` {
		t.Errorf("expected position data, got %q", data)
	}
}

func TestSSEHub_ServeHTTP_MultipleEvents(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	server := httptest.NewServer(hub)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)

	// Пропускаем connected-событие.
	readSSEEvent(t, scanner)
	waitForClientCount(t, hub, 1)

	// Отправляем несколько разных событий.
	hub.Broadcast("position", []byte(`{"norad_id":25544,"lat":47.3}`))
	hub.Broadcast("track", []byte(`{"norad_id":25544,"past":[],"future":[]}`))
	hub.Broadcast("position", []byte(`{"norad_id":25544,"lat":47.4}`))

	// Читаем все три.
	et1, _ := readSSEEvent(t, scanner)
	et2, _ := readSSEEvent(t, scanner)
	et3, d3 := readSSEEvent(t, scanner)

	if et1 != "position" {
		t.Errorf("event 1: expected 'position', got %q", et1)
	}

	if et2 != "track" {
		t.Errorf("event 2: expected 'track', got %q", et2)
	}

	if et3 != "position" {
		t.Errorf("event 3: expected 'position', got %q", et3)
	}

	if !strings.Contains(d3, "47.4") {
		t.Errorf("event 3: expected data with 47.4, got %q", d3)
	}
}

func TestSSEHub_ServeHTTP_ClientDisconnect(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	server := httptest.NewServer(hub)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Ждём регистрации.
	waitForClientCount(t, hub, 1)

	// Закрываем соединение (клиент отключается).
	resp.Body.Close()

	// Клиент должен быть удалён.
	waitForClientCount(t, hub, 0)
}

func TestSSEHub_ServeHTTP_MultipleClients(t *testing.T) {
	hub, cancel := startTestHub(t)
	defer cancel()

	server := httptest.NewServer(hub)
	defer server.Close()

	const numClients = 3
	responses := make([]*http.Response, numClients)
	scanners := make([]*bufio.Scanner, numClients)

	// Подключаем несколько клиентов.
	for i := range numClients {
		resp, err := http.Get(server.URL)
		if err != nil {
			t.Fatalf("client %d: failed to connect: %v", i, err)
		}

		responses[i] = resp
		scanners[i] = bufio.NewScanner(resp.Body)

		// Пропускаем connected-событие.
		readSSEEvent(t, scanners[i])
	}

	defer func() {
		for _, resp := range responses {
			resp.Body.Close()
		}
	}()

	waitForClientCount(t, hub, numClients)

	// Broadcast — все клиенты получают.
	hub.Broadcast("position", []byte(`{"test":true}`))

	for i, scanner := range scanners {
		eventType, _ := readSSEEvent(t, scanner)
		if eventType != "position" {
			t.Errorf("client %d: expected 'position', got %q", i, eventType)
		}
	}
}

// --- Бенчмарки ---

func BenchmarkSSEHub_Broadcast(b *testing.B) {
	hub := NewSSEHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go hub.Run(ctx)

	// Регистрируем 10 клиентов с фоновым чтением.
	for range 10 {
		client := &sseClient{
			events: make(chan SSEEvent, clientEventBufferSize),
		}

		hub.register <- client

		go func(c *sseClient) {
			for range c.events {
			}
		}(client)
	}

	time.Sleep(50 * time.Millisecond)

	data := []byte(`{"norad_id":25544,"lat":47.3,"lon":39.8,"alt":418}`)

	b.ResetTimer()

	for b.Loop() {
		hub.Broadcast("position", data)
	}
}
