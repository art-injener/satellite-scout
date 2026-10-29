package sstv

import (
	"image"
	"image/color"
)

// testBars — цветные полосы испытательной таблицы слева направо.
var testBars = []color.RGBA{
	{255, 255, 255, 255}, {255, 255, 0, 255}, {0, 255, 255, 255}, {0, 255, 0, 255},
	{255, 0, 255, 255}, {255, 0, 0, 255}, {0, 0, 255, 255},
}

// TestPattern — испытательная таблица w×h для имитатора и тестов: цветные полосы на
// две трети высоты, под ними шахматка с клеткой в 1/16 ширины (видна чёткость и наклон),
// внизу серый клин от чёрного к белому.
func TestPattern(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	bars, checks := h*2/3, h*5/6
	cell := max(w/16, 1)
	for y := range h {
		for x := range w {
			var c color.RGBA
			switch {
			case y < bars:
				c = testBars[x*len(testBars)/w]
			case y < checks:
				v := uint8(0)
				if (x/cell+(y-bars)/cell)%2 == 0 {
					v = 255
				}
				c = color.RGBA{v, v, v, 255}
			default:
				v := uint8(x * 255 / max(w-1, 1))
				c = color.RGBA{v, v, v, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}
