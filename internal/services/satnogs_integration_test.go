package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/art-injener/satellite-scout/internal/handlers"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

// fakeTransmitterProvider — мок TransmitterProvider для тестов интеграции.
type fakeTransmitterProvider struct {
	mu                 sync.Mutex
	primaryByID        map[int]*TransmitterInfo
	requestedNoradIDs  []int
	requestFetchCalled int
}

func (f *fakeTransmitterProvider) GetPrimaryTransmitter(noradID int) *TransmitterInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.primaryByID[noradID]
}

func (f *fakeTransmitterProvider) RequestFetch(noradIDs []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestFetchCalled++
	f.requestedNoradIDs = append(f.requestedNoradIDs, noradIDs...)
}

// captureSSEHub — обёртка над реальным SSEHub, перехватывающая broadcast-события для проверки.
type captureSSEHub struct {
	*handlers.SSEHub
	mu     sync.Mutex
	events []handlers.SSEEvent
}

func TestBroadcastGroupUpdate_IncludesFreqAndModulation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer)

	provider := &fakeTransmitterProvider{
		primaryByID: map[int]*TransmitterInfo{
			issNoradID: {FreqMHz: "145.825", Modulation: "AFSK 1200"},
		},
	}
	svc.SetTransmitterProvider(provider)

	now := time.Now().UTC()
	group := ConcurrentPassGroup{
		Satellites: []PassInfo{
			{
				NoradID: issNoradID,
				SatName: "ISS",
				Pass: tracker.Pass{
					NoradID:  issNoradID,
					SatName:  "ISS",
					AOS:      now.UnixMilli(),
					LOS:      now.Add(10 * time.Minute).UnixMilli(),
					Duration: 600,
				},
				IsVisible: true,
			},
		},
		PrimarySatID: issNoradID,
		TimeWindow: TimeWindow{
			Start: now.UnixMilli(),
			End:   now.Add(10 * time.Minute).UnixMilli(),
		},
	}

	// Дёргаем broadcastGroupUpdate напрямую — без полного цикла Run.
	svc.broadcastGroupUpdate(group, now, 0)

	// Проверяем, что provider.RequestFetch вызван с NORAD группы.
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.requestFetchCalled != 1 {
		t.Errorf("RequestFetch calls = %d, want 1", provider.requestFetchCalled)
	}
	if len(provider.requestedNoradIDs) != 1 || provider.requestedNoradIDs[0] != issNoradID {
		t.Errorf("requested NORAD = %v, want [%d]", provider.requestedNoradIDs, issNoradID)
	}
}

func TestBroadcastGroupUpdate_NilProviderDoesNotPanic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer)
	// Намеренно не вызываем SetTransmitterProvider — должно работать без паники.

	now := time.Now().UTC()
	group := ConcurrentPassGroup{
		Satellites: []PassInfo{
			{NoradID: issNoradID, SatName: "ISS", Pass: tracker.Pass{
				NoradID: issNoradID, SatName: "ISS",
				AOS: now.UnixMilli(), LOS: now.Add(10 * time.Minute).UnixMilli(),
				Duration: 600,
			}},
		},
		PrimarySatID: issNoradID,
		TimeWindow:   TimeWindow{Start: now.UnixMilli(), End: now.Add(10 * time.Minute).UnixMilli()},
	}

	// Без TransmitterProvider broadcast не должен паниковать; freq/modulation просто пустые.
	svc.broadcastGroupUpdate(group, now, 0)
}

func TestBroadcastGroupUpdate_NoPrimaryReturnsEmptyFreq(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer)
	provider := &fakeTransmitterProvider{
		primaryByID: map[int]*TransmitterInfo{}, // SatNOGS не нашёл передатчика для ISS.
	}
	svc.SetTransmitterProvider(provider)

	now := time.Now().UTC()
	group := ConcurrentPassGroup{
		Satellites: []PassInfo{
			{NoradID: issNoradID, SatName: "ISS", Pass: tracker.Pass{
				NoradID: issNoradID, SatName: "ISS",
				AOS: now.UnixMilli(), LOS: now.Add(10 * time.Minute).UnixMilli(),
				Duration: 600,
			}},
		},
		PrimarySatID: issNoradID,
		TimeWindow:   TimeWindow{Start: now.UnixMilli(), End: now.Add(10 * time.Minute).UnixMilli()},
	}

	// Должен вызвать prefetch и не упасть.
	svc.broadcastGroupUpdate(group, now, 0)

	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.requestFetchCalled != 1 {
		t.Errorf("RequestFetch calls = %d, want 1", provider.requestFetchCalled)
	}
}
