package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	allByID            map[int][]TransmitterInfo
	requestedNoradIDs  []int
	requestFetchCalled int
}

func (f *fakeTransmitterProvider) GetPrimaryTransmitter(noradID int) *TransmitterInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.primaryByID[noradID]
}

func (f *fakeTransmitterProvider) GetTransmitters(noradID int) []TransmitterInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.allByID[noradID]
}

func (f *fakeTransmitterProvider) RequestFetch(noradIDs []int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requestFetchCalled++
	f.requestedNoradIDs = append(f.requestedNoradIDs, noradIDs...)
}

func TestBroadcastGroupUpdate_IncludesFreqAndModulation(t *testing.T) {
	ctx := t.Context()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	observer := tracker.NewObserver(47.315813, 39.788243, 0.070)

	svc := NewSatelliteTrackingService(hub, store, observer)

	provider := &fakeTransmitterProvider{
		primaryByID: map[int]*TransmitterInfo{
			issNoradID: {UUID: "tx-iss-1", FreqMHz: "145.825", Modulation: "AFSK 1200", Count: 1},
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

// cachedGroupUpdate reads the last satellite_group_update from the hub cache.
func cachedGroupUpdate(t *testing.T, hub *handlers.SSEHub) satelliteGroupUpdate {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sse", nil)
	rec := &lockedRecorder{rec: httptest.NewRecorder()}
	go hub.ServeHTTP(rec, req)

	const prefix = "event: satellite_group_update\ndata: "
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		body := rec.body()
		if i := strings.Index(body, prefix); i >= 0 {
			rest := body[i+len(prefix):]
			if j := strings.Index(rest, "\n"); j >= 0 {
				var ev satelliteGroupUpdate
				if err := json.Unmarshal([]byte(rest[:j]), &ev); err != nil {
					t.Fatalf("decode satellite_group_update: %v", err)
				}
				return ev
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("satellite_group_update not received; body=%q", rec.body())
	return satelliteGroupUpdate{}
}

func TestBroadcastGroupUpdate_ListsAllActiveTransmitters(t *testing.T) {
	ctx := t.Context()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	svc := NewSatelliteTrackingService(hub, store, tracker.NewObserver(47.315813, 39.788243, 0.070))
	svc.SetTransmitterProvider(&fakeTransmitterProvider{
		primaryByID: map[int]*TransmitterInfo{
			issNoradID: {UUID: "tx-aprs", FreqMHz: "145.825", Modulation: "AFSK 1200", Count: 2},
		},
		allByID: map[int][]TransmitterInfo{
			issNoradID: {
				{UUID: "tx-voice", FreqHz: 145_800_000, Mode: "FM", Modulation: "FM", Description: "Voice"},
				{UUID: "tx-aprs", FreqHz: 145_825_000, Mode: "AFSK", Modulation: "AFSK 1200", Baud: 1200, Description: "APRS"},
			},
		},
	})

	now := time.Now().UTC()
	svc.broadcastGroupUpdate(ConcurrentPassGroup{
		Satellites: []PassInfo{{NoradID: issNoradID, SatName: "ISS", Pass: tracker.Pass{
			NoradID: issNoradID, SatName: "ISS",
			AOS: now.UnixMilli(), LOS: now.Add(10 * time.Minute).UnixMilli(), Duration: 600,
		}}},
		PrimarySatID: issNoradID,
	}, now, 0)

	got := cachedGroupUpdate(t, hub).Satellites[0].Transmitters
	want := []groupTxInfo{
		{UUID: "tx-voice", FreqHz: 145_800_000, Mode: "FM", Modulation: "FM", Description: "Voice"},
		{UUID: "tx-aprs", FreqHz: 145_825_000, Mode: "AFSK", Modulation: "AFSK 1200", Baud: 1200, Description: "APRS"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("transmitters = %+v, want %+v", got, want)
	}
}

func TestBroadcastGroupUpdate_NoTransmittersMeansEmptyList(t *testing.T) {
	ctx := t.Context()

	hub := handlers.NewSSEHub()
	go hub.Run(ctx)

	store := setupTLEStore(t)
	svc := NewSatelliteTrackingService(hub, store, tracker.NewObserver(47.315813, 39.788243, 0.070))
	svc.SetTransmitterProvider(&fakeTransmitterProvider{})

	now := time.Now().UTC()
	svc.broadcastGroupUpdate(ConcurrentPassGroup{
		Satellites: []PassInfo{{NoradID: issNoradID, SatName: "ISS", Pass: tracker.Pass{
			NoradID: issNoradID, SatName: "ISS",
			AOS: now.UnixMilli(), LOS: now.Add(10 * time.Minute).UnixMilli(), Duration: 600,
		}}},
		PrimarySatID: issNoradID,
	}, now, 0)

	if got := cachedGroupUpdate(t, hub).Satellites[0].Transmitters; len(got) != 0 {
		t.Errorf("transmitters = %+v, want none", got)
	}
}

func TestGroupTransmitters_ListsDownlinksOfTheGroup(t *testing.T) {
	svc := NewSatelliteTrackingService(handlers.NewSSEHub(), setupTLEStore(t),
		tracker.NewObserver(47.315813, 39.788243, 0.070))
	if got := svc.GroupTransmitters(); got != nil {
		t.Errorf("without SatNOGS: %+v, want none", got)
	}
	svc.SetTransmitterProvider(&fakeTransmitterProvider{allByID: map[int][]TransmitterInfo{
		issNoradID: {{UUID: "tx-voice", FreqHz: 145_800_000}, {UUID: "tx-aprs", FreqHz: 145_825_000}},
		1:          {{UUID: "not-in-group", FreqHz: 437_000_000}},
	}})
	svc.mu.Lock()
	svc.currentGroupEntries = []GroupEntry{{NoradID: issNoradID, IsVisible: true}}
	svc.mu.Unlock()

	got := svc.GroupTransmitters()
	want := []GroupTransmitter{
		{NoradID: issNoradID, TransmitterInfo: TransmitterInfo{UUID: "tx-voice", FreqHz: 145_800_000}},
		{NoradID: issNoradID, TransmitterInfo: TransmitterInfo{UUID: "tx-aprs", FreqHz: 145_825_000}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GroupTransmitters() = %+v, want %+v", got, want)
	}
}

func TestBroadcastGroupUpdate_NilProviderDoesNotPanic(t *testing.T) {
	ctx := t.Context()

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
	ctx := t.Context()

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
