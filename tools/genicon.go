//go:build ignore

// Генератор иконки приложения. Запуск: go run tools/genicon.go
// Рисует build/appicon.png — Wails сам соберёт из него .ico для Windows.
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

const size = 1024

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	const (
		half    = size / 2.0
		margin  = 40.0
		radius  = 232.0
		ringR   = 268.0
		ringW   = 54.0
		dotR    = 96.0
		stroke  = 1.0 // ширина перехода сглаживания в пикселях
		boxHalf = half - margin
	)

	// Градиент акцента: #7C8CFF -> #A78BFA по диагонали.
	from := [3]float64{0x7C, 0x8C, 0xFF}
	to := [3]float64{0xA7, 0x8B, 0xFA}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			// Координаты относительно центра, с отсчётом от угла пикселя.
			px := float64(x) + 0.5 - half
			py := float64(y) + 0.5 - half

			card := sdRoundRect(px, py, boxHalf, boxHalf, radius)
			cardCov := coverage(card, stroke)
			if cardCov <= 0 {
				continue
			}

			// Фон карточки: тёмный, с градиентным свечением сверху.
			t := clamp01((px + py + 2*boxHalf) / (4 * boxHalf))
			bg := mix([3]float64{0x0B, 0x0C, 0x0E}, [3]float64{0x1A, 0x1D, 0x2E}, t*0.9)

			accent := mix(from, to, t)

			// Кольцо фокуса.
			ringSDF := math.Abs(math.Hypot(px, py)-ringR) - ringW/2
			ringCov := coverage(ringSDF, stroke)

			// Центральная точка.
			dotCov := coverage(math.Hypot(px, py)-dotR, stroke)

			glyph := math.Max(ringCov, dotCov)

			c := mix(bg, accent, 0.18)                      // мягкая подложка
			c = mix(c, [3]float64{0xFF, 0xFF, 0xFF}, glyph) // белый глиф

			img.SetNRGBA(x, y, color.NRGBA{
				R: clamp8(c[0]),
				G: clamp8(c[1]),
				B: clamp8(c[2]),
				A: uint8(math.Round(cardCov * 255)),
			})
		}
	}

	out := filepath.Join("build", "appicon.png")
	if err := os.MkdirAll("build", 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(out)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("иконка записана: %s", out)
}

// sdRoundRect — знаковая функция расстояния до скруглённого прямоугольника.
func sdRoundRect(px, py, halfW, halfH, r float64) float64 {
	qx := math.Abs(px) - halfW + r
	qy := math.Abs(py) - halfH + r
	return math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - r
}

// coverage превращает расстояние в долю покрытия пикселя [0,1].
func coverage(d, width float64) float64 {
	return clamp01(0.5 - d/width)
}

func clamp01(v float64) float64 {
	return math.Min(1, math.Max(0, v))
}

func clamp8(v float64) uint8 {
	return uint8(math.Round(math.Min(255, math.Max(0, v))))
}

func mix(a, b [3]float64, t float64) [3]float64 {
	t = clamp01(t)
	return [3]float64{
		a[0] + (b[0]-a[0])*t,
		a[1] + (b[1]-a[1])*t,
		a[2] + (b[2]-a[2])*t,
	}
}
