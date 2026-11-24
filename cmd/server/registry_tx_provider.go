package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/art-injener/satellite-scout/internal/registry"
	"github.com/art-injener/satellite-scout/internal/satnogs"
	"github.com/art-injener/satellite-scout/internal/services"
)

// registryTXSource — передатчики из реестра для SSE и REST. Сеть не дергает.
type registryTXSource struct {
	reg *registry.Registry
}

func newRegistryTXSource(reg *registry.Registry) *registryTXSource {
	if reg == nil {
		return nil
	}
	return &registryTXSource{reg: reg}
}

// asServices — TransmitterProvider для tracking / planAugment.
func (s *registryTXSource) asServices() services.TransmitterProvider {
	if s == nil {
		return nil
	}
	return &registryServicesTX{src: s}
}

// asREST — каталог для GET /api/satnogs/transmitters (имя пути историческое).
func (s *registryTXSource) asREST() *registryRESTTX {
	if s == nil {
		return nil
	}
	return &registryRESTTX{src: s}
}

func (s *registryTXSource) list(noradID int) []services.TransmitterInfo {
	if s == nil || s.reg == nil {
		return nil
	}
	txs := s.reg.Transmitters(noradID)
	if len(txs) == 0 {
		return nil
	}
	out := make([]services.TransmitterInfo, len(txs))
	for i, tx := range txs {
		out[i] = services.TransmitterInfo{
			UUID:        tx.UUID,
			FreqHz:      tx.FreqHz,
			FreqMHz:     formatTXMHz(tx.FreqHz),
			Mode:        tx.Mode,
			Modulation:  formatTXModulation(tx.Mode, tx.Baud),
			Baud:        tx.Baud,
			Description: tx.Name,
		}
	}
	slices.SortFunc(out, func(a, b services.TransmitterInfo) int {
		switch {
		case a.FreqHz < b.FreqHz:
			return -1
		case a.FreqHz > b.FreqHz:
			return 1
		default:
			return 0
		}
	})
	for i := range out {
		out[i].Count = len(out)
	}
	return out
}

type registryServicesTX struct {
	src *registryTXSource
}

func (a *registryServicesTX) GetTransmitters(noradID int) []services.TransmitterInfo {
	return a.src.list(noradID)
}

func (a *registryServicesTX) GetPrimaryTransmitter(noradID int) *services.TransmitterInfo {
	list := a.src.list(noradID)
	if len(list) == 0 {
		return nil
	}
	tx := list[0]
	return &tx
}

// RequestFetch пустой: пробелы SatNOGS догружает feedRegistry, не UI.
func (a *registryServicesTX) RequestFetch([]int) {}

type registryRESTTX struct {
	src *registryTXSource
}

func (a *registryRESTTX) GetAllTransmitters(noradID int) []satnogs.Transmitter {
	list := a.src.list(noradID)
	if len(list) == 0 {
		return nil
	}
	out := make([]satnogs.Transmitter, len(list))
	for i, tx := range list {
		hz := tx.FreqHz
		baud := tx.Baud
		out[i] = satnogs.Transmitter{
			UUID:        tx.UUID,
			Description: tx.Description,
			Alive:       true,
			Status:      "active",
			DownlinkLow: &hz,
			Mode:        tx.Mode,
			Baud:        &baud,
			NoradCatID:  noradID,
		}
	}
	return out
}

func (a *registryRESTTX) GetPrimaryTransmitter(noradID int) *satnogs.TransmitterSummary {
	return satnogs.SelectPrimary(a.GetAllTransmitters(noradID))
}

func formatTXMHz(hz int64) string {
	if hz <= 0 {
		return ""
	}
	return strconv.FormatFloat(float64(hz)/1e6, 'f', 3, 64)
}

func formatTXModulation(mode string, baud float64) string {
	mode = strings.TrimSpace(mode)
	if baud <= 0 {
		return mode
	}
	return strings.TrimSpace(fmt.Sprintf("%s %s", mode, formatTXBaud(baud)))
}

func formatTXBaud(b float64) string {
	if b <= 0 {
		return ""
	}
	if b >= 10000 {
		k := b / 1000
		intK := int(k)
		rem := int(k*10+0.5) % 10
		if rem == 0 {
			return fmt.Sprintf("%dk", intK)
		}
		return fmt.Sprintf("%dk%d", intK, rem)
	}
	return strconv.FormatInt(int64(b+0.5), 10)
}
