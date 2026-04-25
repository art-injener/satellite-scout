package services

import (
	"sync"
	"time"
)

// clientState — состояние одного клиента UI.
type clientState struct {
	TrackingID int       // NORAD ID спутника под наблюдением (0 = нет).
	LastSeen   time.Time // Время последней активности (для TTL).
}

// ClientStateStore — per-client хранилище tracking_id.
// Потокобезопасное, с автоочисткой по TTL.
type ClientStateStore struct {
	mu      sync.RWMutex
	clients map[string]*clientState
	ttl     time.Duration
}

// NewClientStateStore создаёт хранилище с заданным TTL неактивных клиентов.
func NewClientStateStore(ttl time.Duration) *ClientStateStore {
	return &ClientStateStore{
		clients: make(map[string]*clientState),
		ttl:     ttl,
	}
}

// SetTracking устанавливает tracking_id для клиента.
func (s *ClientStateStore) SetTracking(clientID string, noradID int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs, ok := s.clients[clientID]
	if !ok {
		cs = &clientState{}
		s.clients[clientID] = cs
	}
	cs.TrackingID = noradID
	cs.LastSeen = time.Now()
}

// ClearTracking сбрасывает tracking_id для клиента.
func (s *ClientStateStore) ClearTracking(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.clients[clientID]; ok {
		cs.TrackingID = 0
		cs.LastSeen = time.Now()
	}
}

// ClearTrackingForNorad сбрасывает наблюдение у всех клиентов с данным NORAD (окончание сеанса).
func (s *ClientStateStore) ClearTrackingForNorad(noradID int) {
	if noradID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cs := range s.clients {
		if cs.TrackingID == noradID {
			cs.TrackingID = 0
			cs.LastSeen = time.Now()
		}
	}
}

// GetTracking возвращает tracking_id для клиента (0 если нет).
func (s *ClientStateStore) GetTracking(clientID string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cs, ok := s.clients[clientID]; ok {
		return cs.TrackingID
	}
	return 0
}

// Touch обновляет LastSeen для клиента (вызывается при SSE connect).
func (s *ClientStateStore) Touch(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.clients[clientID]; ok {
		cs.LastSeen = time.Now()
	} else {
		s.clients[clientID] = &clientState{LastSeen: time.Now()}
	}
}

// Cleanup удаляет клиентов, неактивных дольше TTL.
func (s *ClientStateStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-s.ttl)
	removed := 0
	for id, cs := range s.clients {
		if cs.LastSeen.Before(cutoff) {
			delete(s.clients, id)
			removed++
		}
	}
	return removed
}

// ClientCount возвращает количество хранимых клиентов.
func (s *ClientStateStore) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}
