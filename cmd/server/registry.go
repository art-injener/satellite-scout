package main

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"github.com/art-injener/satellite-scout/internal/config"
	"github.com/art-injener/satellite-scout/internal/freqplan"
	"github.com/art-injener/satellite-scout/internal/registry"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/sdr"
	"github.com/art-injener/satellite-scout/internal/sdr/chipdrivers/rtlsdr"
	"github.com/art-injener/satellite-scout/internal/sdr/imitator"
	"github.com/art-injener/satellite-scout/internal/services"
	"github.com/art-injener/satellite-scout/internal/tracker"
)

const (
	registryDBPath   = "data/scout.db"
	sonikTxCachePath = "data/sonik_transmitters.json"
)

// openRegistry открывает реестр КА. Без базы реестр работает в памяти: после рестарта
// наполнится заново из кешей источников, но сошедшие КА до прохода SATCAT не отсекутся.
func openRegistry() *registry.Registry {
	reg, err := registry.Open(context.Background(), registryDBPath)
	if err != nil {
		slog.Warn("registry: database unavailable, keeping registry in memory",
			slog.String("path", registryDBPath), slog.Any(slogKeyError, err))
		return registry.New()
	}
	slog.Info("registry opened", slog.String("path", registryDBPath))
	return reg
}

// registrySink передаёт реестру итог прохода каталога: элементы GP, каталог sonik и SATCAT.
func registrySink(reg *registry.Registry) tracker.CatalogSink {
	return func(ctx context.Context, p tracker.CatalogPass) {
		if err := reg.IngestCatalogPass(ctx, p); err != nil {
			slog.WarnContext(ctx, "registry: catalog pass", slog.Any(slogKeyError, err))
		}
	}
}

// passExcluder убирает из пролётов КА, исключённые оператором, и сошедшие с орбиты.
// Отбор «кого слушать» — метка группы «над нами», не фильтр пролётов.
type passExcluder struct {
	manual services.Excluder
	reg    *registry.Registry
}

func (e passExcluder) Contains(norad int) bool {
	return e.manual.Contains(norad) || e.reg.Decayed(norad)
}

// bandSource отдаёт плану частот передатчики в диапазоне приёмников.
// Полоса антенны не отсекает — только поднимает совпавшие частоты выше остальных.
type bandSource struct {
	src     freqplan.Source
	bands   registry.Bands
	antenna registry.Bands
}

func (b bandSource) Transmitters(norad int) []freqplan.Tx {
	txs := slices.Clone(b.src.Transmitters(norad))
	txs = slices.DeleteFunc(txs, func(tx freqplan.Tx) bool {
		return !b.bands.Allow(tx.FreqHz)
	})
	if len(b.antenna) == 0 || len(txs) < 2 {
		return txs
	}
	slices.SortStableFunc(txs, func(a, c freqplan.Tx) int {
		ain, cin := b.antenna.Allow(a.FreqHz), b.antenna.Allow(c.FreqHz)
		switch {
		case ain == cin:
			return 0
		case ain:
			return -1
		default:
			return 1
		}
	})
	return txs
}

// stationBands собирает диапазоны перестройки приёмников радиотрактов по возможностям драйвера.
// Драйвер без реализации диапазона не даёт: его тракт не отсекает частоты других трактов.
func stationBands(paths []config.RadioPath) registry.Bands {
	out := make(registry.Bands, 0, len(paths))
	for _, rp := range paths {
		var caps sdr.Capabilities
		switch rp.Receiver.Driver {
		case "imitator", config.DriverSimulated, "":
			caps = imitator.New(imitator.Options{}).Capabilities()
		case "rtlsdr":
			caps = rtlsdr.New(rtlsdr.Options{}).Capabilities()
		default:
			continue
		}
		out = append(out, registry.Band{
			MinHz: int64(caps.MinFreqHz), //nolint:gosec // частоты приёмника на порядки меньше предела int64
			MaxHz: int64(caps.MaxFreqHz), //nolint:gosec // частоты приёмника на порядки меньше предела int64
		})
	}
	return out
}

// stationAntennaBands собирает рабочие полосы антенн трактов (МГц конфига → Гц).
// Пустая полоса у тракта не участвует: сортировка по антенне для него не задана.
func stationAntennaBands(paths []config.RadioPath) registry.Bands {
	out := make(registry.Bands, 0, len(paths))
	for _, rp := range paths {
		if rp.Antenna.FreqMinMHz == nil || rp.Antenna.FreqMaxMHz == nil {
			continue
		}
		out = append(out, registry.Band{
			MinHz: int64(*rp.Antenna.FreqMinMHz * 1e6),
			MaxHz: int64(*rp.Antenna.FreqMaxMHz * 1e6),
		})
	}
	return out
}

// groupLabelFilter отбирает КА группы «над нами» по метке реестра.
type groupLabelFilter struct {
	reg   *registry.Registry
	bands registry.Bands
	label atomic.Value // string
}

func newGroupLabelFilter(reg *registry.Registry, bands registry.Bands, label string) *groupLabelFilter {
	f := &groupLabelFilter{reg: reg, bands: bands}
	f.SetLabel(label)
	return f
}

func (f *groupLabelFilter) SetLabel(label string) {
	f.label.Store(registry.NormalizeGroupLabel(label))
}

func (f *groupLabelFilter) Include(norad int) bool {
	label, _ := f.label.Load().(string)
	return f.reg.MatchGroupLabel(norad, label, f.bands)
}

// syncRegistryOrbits переносит в реестр элементы хранилища TLE (кеш на диске, Celestrak).
// Реестр оставляет более свежую эпоху, поэтому повтор после прохода Space-Track ничего не меняет.
func (a *app) syncRegistryOrbits(ctx context.Context) {
	now := time.Now()
	source := a.tleStore.Status(now).Source
	if err := a.registry.PutOrbits(ctx, source, a.tleStore.GetAll(), now); err != nil {
		slog.WarnContext(ctx, "registry: orbits", slog.Any(slogKeyError, err))
	}
}

// seedCompositionBeforeCatalog наполняет основу состава каталога с диска до первого GP.
// Нужно, чтобы Space-Track не ушёл с пустым набором, пока startFreqPlan ещё не крутился.
func (a *app) seedCompositionBeforeCatalog(ctx context.Context) {
	curated, err := freqplan.LoadCurated(freqCuratedPath)
	if err != nil {
		slog.WarnContext(ctx, "registry: curated seed", slog.Any(slogKeyError, err))
	} else {
		a.seedRegistry(ctx, curated)
	}
	if amsat, err := freqplan.LoadAMSATFile(freqAMSATCache); err != nil {
		slog.WarnContext(ctx, "registry: amsat cache seed", slog.Any(slogKeyError, err))
	} else {
		a.feedSnapshot(ctx, registry.SourceAMSAT, amsat.All(), true, nil, fileTime(freqAMSATCache))
	}
	if ha, err := loadHeavensAboveCache(freqHACache); err != nil {
		slog.WarnContext(ctx, "registry: heavens-above cache seed", slog.Any(slogKeyError, err))
	} else {
		a.feedSnapshot(ctx, registry.SourceHeavensAbove, ha.All(), true, nil, fileTime(freqHACache))
	}
}

// loadHeavensAboveCache читает сохранённую страницу HA без сети.
func loadHeavensAboveCache(path string) (*freqplan.HeavensAbove, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return freqplan.ParseHeavensAbove(f, info.ModTime())
}

// seedRegistry кладёт в реестр источники, которые на старте уже лежат на диске: curated и кеш SatNOGS.
func (a *app) seedRegistry(ctx context.Context, curated *freqplan.Curated) {
	at := fileTime(freqCuratedPath)
	if err := a.registry.ReplaceTransmitters(
		ctx,
		registry.SourceCurated,
		registry.FromFreqplan(curated.All(), at),
		at,
	); err != nil {
		slog.WarnContext(ctx, "registry: curated", slog.Any(slogKeyError, err))
	}
	if a.satnogs == nil {
		return
	}
	entries, err := satnogs.ReadDiskCache(satnogsCachePath)
	if err != nil {
		slog.WarnContext(ctx, "registry: satnogs cache", slog.Any(slogKeyError, err))
		return
	}
	byNorad := make(map[int][]registry.Transmitter, len(entries))
	for id, e := range entries {
		byNorad[id] = registry.FromSatnogs(id, e.Transmitters, e.FetchedAt)
	}
	if err = a.registry.ReplaceTransmittersByNorad(ctx, registry.SourceSatNOGS, byNorad, time.Now()); err != nil {
		slog.WarnContext(ctx, "registry: satnogs cache", slog.Any(slogKeyError, err))
	}
}

// feedRegistry обновляет передатчики реестра перед сборкой плана частот.
// Источник, который не ответил и не оставил кеша, сохраняет в реестре прежний снимок.
func (a *app) feedRegistry(ctx context.Context, ha *freqplan.HeavensAbove, haErr error,
	amsat *freqplan.AMSAT, amsatErr error, satnogsIDs []int,
) {
	now := time.Now()
	a.feedSnapshot(ctx, registry.SourceHeavensAbove, ha.All(), ha != nil, haErr, fileTime(freqHACache))
	a.feedSnapshot(
		ctx,
		registry.SourceAMSAT,
		amsat.All(),
		amsat != nil && amsatErr == nil,
		amsatErr,
		fileTime(freqAMSATCache),
	)

	sonikCtx, cancel := context.WithTimeout(ctx, freqFetchTimeout)
	txs, at, err := registry.NewSonikTxClient().Load(sonikCtx, sonikTxCachePath, registry.DefaultSonikTxTTL)
	cancel()
	if err != nil {
		slog.WarnContext(ctx, "registry: sonik transmitters", slog.Any(slogKeyError, err))
	}
	a.markLoad(ctx, registry.SourceSonikTx, now, err)
	if len(txs) > 0 {
		if putErr := a.registry.ReplaceTransmitters(ctx, registry.SourceSonik, txs, at); putErr != nil {
			slog.WarnContext(ctx, "registry: sonik transmitters", slog.Any(slogKeyError, putErr))
		}
	}

	if a.satnogs == nil {
		return
	}
	requestSatNOGSGaps(a.registry, a.satnogsAdapter, satnogsIDs)
	byNorad := satnogsSnapshot(satnogsIDs, a.satnogs.GetAllTransmitters, now)
	if err = a.registry.ReplaceTransmittersByNorad(ctx, registry.SourceSatNOGS, byNorad, now); err != nil {
		slog.WarnContext(ctx, "registry: satnogs", slog.Any(slogKeyError, err))
	}
}

// satnogsFetcher очередь догрузки передатчиков SatNOGS (адаптер или тест-шпион).
type satnogsFetcher interface {
	RequestFetch([]int)
}

// requestSatNOGSGaps ставит в очередь только NORAD без curated/HA/AMSAT/sonik.
func requestSatNOGSGaps(reg *registry.Registry, fetch satnogsFetcher, ids []int) {
	if reg == nil || fetch == nil {
		return
	}
	gaps := reg.FilterSatNOGSGaps(ids)
	if len(gaps) == 0 {
		return
	}
	fetch.RequestFetch(gaps)
}

// feedSnapshot заменяет снимок источника, если он есть, и пишет журнал загрузки.
func (a *app) feedSnapshot(ctx context.Context, source string, byNorad map[int][]freqplan.Tx, ok bool,
	fetchErr error, at time.Time,
) {
	a.markLoad(ctx, source, time.Now(), fetchErr)
	if !ok {
		return
	}
	if err := a.registry.ReplaceTransmitters(ctx, source, registry.FromFreqplan(byNorad, at), at); err != nil {
		slog.WarnContext(ctx, "registry: "+source, slog.Any(slogKeyError, err))
	}
}

func (a *app) markLoad(ctx context.Context, source string, at time.Time, fetchErr error) {
	if err := a.registry.MarkLoad(ctx, source, at, fetchErr); err != nil {
		slog.WarnContext(ctx, "registry: load journal", slog.Any(slogKeyError, err))
	}
}

// satnogsSnapshot берёт из кеша SatNOGS передатчики перечисленных КА. КА, о которых кеш
// ничего не знает, пропускаются: их прежние записи в реестре остаются.
func satnogsSnapshot(ids []int, get func(int) []satnogs.Transmitter, at time.Time) map[int][]registry.Transmitter {
	out := make(map[int][]registry.Transmitter, len(ids))
	for _, id := range ids {
		if list := get(id); len(list) > 0 {
			out[id] = registry.FromSatnogs(id, list, at)
		}
	}
	return out
}

// fileTime время изменения файла кеша; нет файла — текущее время.
func fileTime(path string) time.Time {
	if info, err := os.Stat(path); err == nil {
		return info.ModTime()
	}
	return time.Now()
}
