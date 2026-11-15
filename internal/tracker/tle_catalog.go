package tracker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// catalogCacheName — файл каталога с элементами Space-Track (состав по ADR-011 §2.7).
// Ключ в cache_meta и в сведениях о загрузке совпадает с именем файла.
const catalogCacheName = "catalog"

// WithSpaceTrackClient делает Space-Track первым источником элементов для всего каталога.
func WithSpaceTrackClient(client *SpaceTrackClient) TLEStoreOption {
	return func(s *TLEStore) {
		s.spacetrack = client
	}
}

// WithSonikClient подключает справочник sonik: снимок КА в проход каталога и свежие запуски в состав.
func WithSonikClient(client *SonikClient) TLEStoreOption {
	return func(s *TLEStore) {
		s.sonik = client
	}
}

// Composition возвращает NORAD основы каталога (curated, Heavens-Above, AMSAT).
type Composition func() []int

// WithComposition задаёт основу набора NORAD для Space-Track (без подмешивания старого catalog).
func WithComposition(fn Composition) TLEStoreOption {
	return func(s *TLEStore) {
		s.composition = fn
	}
}

// CatalogPass итог одного прохода каталога: всё, что загрузили по пути, для реестра КА.
type CatalogPass struct {
	At          time.Time
	TLEs        []*TLE // ответ GP; пусто при ошибке
	Err         error  // ошибка GP
	Sonik       []SonikSat
	SonikErr    error
	Satcat      []SatcatRecord
	SatcatBytes int64
	SatcatErr   error
}

// CatalogSink получает итог прохода каталога до уведомления подписчиков SetOnRefresh.
type CatalogSink func(ctx context.Context, pass CatalogPass)

// WithCatalogSink передаёт итоги проходов каталога (элементы, sonik, SATCAT) реестру КА.
func WithCatalogSink(sink CatalogSink) TLEStoreOption {
	return func(s *TLEStore) {
		s.sink = sink
	}
}

// GetUngrouped возвращает TLE, не входящие ни в одну группу (например, известные только из sonik).
func (s *TLEStore) GetUngrouped() []*TLE {
	s.mu.RLock()
	defer s.mu.RUnlock()

	grouped := make(map[int]struct{}, len(s.catalog))
	for _, ids := range s.byGroup {
		for _, id := range ids {
			grouped[id] = struct{}{}
		}
	}
	var tles []*TLE
	for id, tle := range s.catalog {
		if _, ok := grouped[id]; !ok {
			tles = append(tles, tle)
		}
	}
	slices.SortFunc(tles, func(a, b *TLE) int { return a.NoradID - b.NoradID })
	return tles
}

// celestrakFetchName — ключ сведений о запасной загрузке орбит с Celestrak по NORAD.
const celestrakFetchName = "celestrak"

// refresh — один проход обновления: Space-Track по каталогу;
// при отказе или без учётки — Celestrak по NORAD состава (не по группам).
func (s *TLEStore) refresh(ctx context.Context) {
	if s.spacetrack != nil {
		err := s.refreshCatalog(ctx)
		if err == nil || ctx.Err() != nil {
			return
		}
		s.logger.WarnContext(ctx, "space-track refresh failed, trying celestrak by NORAD", slog.Any(slogKeyError, err))
	}
	if err := s.refreshCelestrakByNorad(ctx); err != nil && ctx.Err() == nil {
		s.logger.WarnContext(ctx, "celestrak NORAD refresh had errors", slog.Any(slogKeyError, err))
	}
}

// refreshCelestrakByNorad подтягивает орбиты с Celestrak по одному NORAD из состава каталога.
// Группы Celestrak (GROUP=…) больше не задают состав — только запасной путь по ID.
func (s *TLEStore) refreshCelestrakByNorad(ctx context.Context) error {
	pass := CatalogPass{At: time.Now()}
	ids := s.catalogNoradIDs(ctx, &pass)
	if len(ids) == 0 {
		s.mu.RLock()
		ids = slices.Collect(maps.Keys(s.catalog))
		s.mu.RUnlock()
		slices.Sort(ids)
	}
	if len(ids) == 0 {
		s.logger.WarnContext(ctx, "celestrak NORAD refresh skipped: no composition and empty catalog")
		return nil
	}

	s.beginLoad(TLELoadPhaseCelestrak, len(ids))
	defer s.endLoad()

	got := make([]*TLE, 0, len(ids))
	var firstErr error
	for i, id := range ids {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		tle, err := s.client.FetchByNoradID(ctx, id)
		s.reportLoadProgress(i+1, len(ids))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.DebugContext(ctx, "celestrak NORAD miss",
				slog.Int("norad_id", id), slog.Any(slogKeyError, err))
			continue
		}
		got = append(got, tle)
	}

	if len(got) == 0 {
		fetchErr := ""
		if firstErr != nil {
			fetchErr = firstErr.Error()
		}
		s.mu.Lock()
		s.setFetch(celestrakFetchName, func(f *groupFetch) {
			f.lastErr = fetchErr
		})
		s.mu.Unlock()
		if firstErr != nil {
			return firstErr
		}
		return nil
	}

	now := time.Now()
	s.mu.Lock()
	for _, tle := range got {
		s.addInternal(tle, "")
	}
	s.setFetch(celestrakFetchName, func(f *groupFetch) {
		f.source = TLESourceCelestrak
		f.fetchedAt = now
		f.lastErr = ""
	})
	all := make([]*TLE, 0, len(s.catalog))
	for _, tle := range s.catalog {
		all = append(all, tle)
	}
	s.mu.Unlock()

	if err := s.saveGroupToCache(catalogCacheName, all); err != nil {
		s.logger.WarnContext(ctx, "failed to save catalog after celestrak", slog.Any(slogKeyError, err))
	}

	s.logger.InfoContext(ctx, "loaded TLE from celestrak by NORAD",
		slog.Int("requested", len(ids)), slog.Int("received", len(got)))
	return nil
}

// refreshCatalog обновляет элементы каталога одним проходом Space-Track.
// Набор NORAD собирается заново (состав из реестра и свежие запуски sonik), без объединения
// со старым catalog. Для запрошенных NORAD без ответа остаются прежние элементы.
// После удачного GP из памяти и файла уходит всё, чего нет в наборе.
// В том же проходе запрашиваются даты схода (SATCAT); их отказ проход не обрывает.
func (s *TLEStore) refreshCatalog(ctx context.Context) error {
	pass := CatalogPass{At: time.Now()}
	ids := s.catalogNoradIDs(ctx, &pass)
	if len(ids) == 0 {
		s.logger.WarnContext(ctx, "TLE catalog composition is empty, keeping cached elements")
		s.sendPass(ctx, pass)
		return nil
	}
	batches := spaceTrackBatchCount(len(ids), s.spacetrack.BatchSize())
	s.beginLoad(TLELoadPhaseGP, batches)
	defer s.endLoad()

	tles, err := s.spacetrack.FetchByNoradIDsProgress(ctx, ids, s.reportLoadProgress)
	if err != nil {
		fetchErr := err.Error()
		s.mu.Lock()
		s.setFetch(catalogCacheName, func(f *groupFetch) {
			f.lastErr = fetchErr
			if f.source == "" {
				f.source = TLESourceCache
			}
		})
		s.mu.Unlock()
		pass.Err = err
		s.sendPass(ctx, pass)
		return fmt.Errorf("catalog refresh: %w", err)
	}
	keep := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		keep[id] = struct{}{}
	}
	s.mu.Lock()
	for _, tle := range tles {
		s.addInternal(tle, "")
	}
	removed := s.retainOnlyLocked(keep)
	all := slices.Collect(maps.Values(s.catalog))
	s.mu.Unlock()

	if saveErr := s.saveGroupToCache(catalogCacheName, all); saveErr != nil {
		s.logger.WarnContext(ctx, "failed to save TLE catalog cache", slog.Any(slogKeyError, saveErr))
	}

	s.mu.Lock()
	s.setFetch(catalogCacheName, func(f *groupFetch) {
		f.source = TLESourceSpaceTrack
		f.fetchedAt = time.Now()
		f.lastErr = ""
	})
	s.mu.Unlock()

	s.logger.InfoContext(ctx, "loaded TLE catalog from Space-Track",
		slog.Int("requested", len(ids)),
		slog.Int("received", len(tles)),
		slog.Int("removed", removed),
		slog.Int("total", len(all)),
	)

	pass.TLEs = tles
	s.fetchSatcat(ctx, ids, &pass)
	s.sendPass(ctx, pass)
	return nil
}

// fetchSatcat дополняет проход датами схода с орбиты; ошибка только записывается в итог.
func (s *TLEStore) fetchSatcat(ctx context.Context, ids []int, pass *CatalogPass) {
	batches := spaceTrackBatchCount(len(ids), s.spacetrack.BatchSize())
	s.beginLoad(TLELoadPhaseSatcat, batches)
	recs, n, err := s.spacetrack.FetchSatcatProgress(ctx, ids, s.reportLoadProgress)
	pass.Satcat, pass.SatcatBytes = recs, n
	if len(recs) > 0 {
		decayed := 0
		for _, r := range recs {
			if !r.Decay.IsZero() {
				decayed++
			}
		}
		s.logger.InfoContext(ctx, "loaded SATCAT from Space-Track",
			slog.Int("records", len(recs)),
			slog.Int("decayed", decayed),
			slog.Int64("bytes", n),
		)
	}
	if err != nil {
		pass.SatcatErr = err
		s.logger.WarnContext(ctx, "space-track satcat failed", slog.Any(slogKeyError, err))
	}
}

func (s *TLEStore) sendPass(ctx context.Context, pass CatalogPass) {
	if s.sink != nil {
		s.sink(ctx, pass)
	}
}

// catalogNoradIDs собирает набор NORAD для Space-Track заново и кладёт снимок sonik в итог прохода.
// Основа — composition (curated, HA, AMSAT); sonik добавляет только свежие запуски.
// Старый catalog и группы Celestrak в набор не входят.
func (s *TLEStore) catalogNoradIDs(ctx context.Context, pass *CatalogPass) []int {
	var ids []int
	if s.composition != nil {
		ids = append(ids, s.composition()...)
	}
	if s.sonik != nil {
		sats, err := s.sonik.FetchCatalog(ctx)
		if err != nil {
			pass.SonikErr = err
			s.logger.WarnContext(ctx, "sonik catalog unavailable, composition without fresh launches",
				slog.Any(slogKeyError, err))
		} else {
			pass.Sonik = sats
			ids = append(ids, SonikFreshLaunchIDs(sats, time.Now())...)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// retainOnlyLocked оставляет в каталоге только NORAD из keep; индексы групп и имён чистит.
// Вызывать под s.mu.
func (s *TLEStore) retainOnlyLocked(keep map[int]struct{}) int {
	removed := 0
	for id := range s.catalog {
		if _, ok := keep[id]; ok {
			continue
		}
		s.removeInternalLocked(id)
		removed++
	}
	return removed
}

// removeInternalLocked удаляет КА из каталога и индексов. Вызывать под s.mu.
func (s *TLEStore) removeInternalLocked(id int) {
	tle := s.catalog[id]
	delete(s.catalog, id)
	delete(s.metadata, id)
	if tle != nil && tle.Name != "" {
		s.removeFromIndex(s.byName, strings.ToLower(tle.Name), id)
	}
	for group, ids := range s.byGroup {
		filtered := slices.DeleteFunc(slices.Clone(ids), func(x int) bool { return x == id })
		if len(filtered) == 0 {
			delete(s.byGroup, group)
		} else {
			s.byGroup[group] = filtered
		}
	}
}

// removeFromIndex убирает ID из индекса по ключу.
func (s *TLEStore) removeFromIndex(index map[string][]int, key string, id int) {
	ids := index[key]
	filtered := slices.DeleteFunc(slices.Clone(ids), func(x int) bool { return x == id })
	if len(filtered) == 0 {
		delete(index, key)
		return
	}
	index[key] = filtered
}

// loadCatalogCache поднимает каталог с диска поверх кешей групп: его элементы свежее.
// Метки групп не трогает.
func (s *TLEStore) loadCatalogCache() {
	tles, err := s.loadGroupFromCache(catalogCacheName)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			s.logger.Warn("failed to load TLE catalog cache", slog.Any(slogKeyError, err))
		}
		return
	}
	meta, err := s.loadCacheMeta()
	if err != nil {
		meta = &CacheMeta{}
	}

	s.mu.Lock()
	for _, tle := range tles {
		s.addInternal(tle, "")
	}
	s.setFetch(catalogCacheName, func(f *groupFetch) {
		f.source = TLESourceCache
		f.fetchedAt = meta.Groups[catalogCacheName].UpdatedAt
	})
	s.mu.Unlock()

	s.logger.Info("loaded TLE catalog from cache", slog.Int("count", len(tles)))
}

// catalogNeedsNetwork сообщает, что файл каталога отсутствует или старше интервала обновления.
// Одной метки в cache_meta мало: без catalog.tle сеть всё равно нужна.
func (s *TLEStore) catalogNeedsNetwork() bool {
	cachePath := filepath.Join(s.config.CacheDir, catalogCacheName+tleCacheExtension)
	if _, err := os.Stat(cachePath); err != nil {
		return true
	}
	meta, err := s.loadCacheMeta()
	if err != nil {
		return true
	}
	return !s.cacheWithinInterval(meta, catalogCacheName)
}

// needsNetworkRefresh — нужен ли сетевой проход при старте.
func (s *TLEStore) needsNetworkRefresh() bool {
	if s.spacetrack != nil {
		return s.catalogNeedsNetwork()
	}
	return s.celestrakNeedsNetwork()
}

// celestrakNeedsNetwork — без Space-Track: пустой каталог или протухший запасной снимок.
func (s *TLEStore) celestrakNeedsNetwork() bool {
	if s.Count() == 0 {
		return true
	}
	meta, err := s.loadCacheMeta()
	if err != nil {
		return true
	}
	if s.cacheWithinInterval(meta, catalogCacheName) {
		return false
	}
	for _, g := range DefaultTLEGroups {
		if s.cacheWithinInterval(meta, g) {
			return false
		}
	}
	s.mu.RLock()
	f := s.fetch[celestrakFetchName]
	s.mu.RUnlock()
	if !f.fetchedAt.IsZero() && s.config.UpdateInterval > 0 &&
		time.Since(f.fetchedAt) < s.config.UpdateInterval {
		return false
	}
	return true
}
