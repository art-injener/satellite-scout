// Package sweep — обход тракта по диапазонам из конфига со стоянкой на каждом окне
// и снимки спектра окон для панорамы (ADR-004 §2.4, §4.4).
package sweep

import (
	"slices"
)

// Range — диапазон обхода: CenterHz > 0 — одно окно, иначе края LoHz..HiHz.
type Range struct {
	CenterHz   uint64
	LoHz, HiHz uint64
}

// Windows режет диапазоны на окна шириной bandHz и возвращает их центры по возрастанию
// без повторов. Шаг центров — 4/5 полосы, соседние окна перекрываются на 1/5 и
// симметричны относительно середины диапазона. Панорама сшивает перекрытие
// плавно. Диапазон уже полосы даёт одно окно по его середине.
func Windows(ranges []Range, bandHz uint32) []uint64 {
	if bandHz == 0 {
		return nil
	}
	band := uint64(bandHz)
	// 4/5 полосы. У полосы короче 5 Гц шаг обнулился бы, тогда окна ставятся без перекрытия.
	step := band * 4 / 5
	if step == 0 {
		step = band
	}
	var out []uint64
	for _, r := range ranges {
		if r.CenterHz > 0 {
			out = append(out, r.CenterHz)
			continue
		}
		if r.HiHz <= r.LoHz {
			continue
		}
		width := r.HiHz - r.LoHz
		n := uint64(1)
		if width > band {
			n = (width-band+step-1)/step + 1
		}
		mid := (r.LoHz + r.HiHz) / 2
		first := mid - (n-1)*step/2
		for i := range n {
			out = append(out, first+i*step)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}
