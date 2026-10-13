//go:build !windows

// Implementación de prueba para desarrollar fuera de Windows:
// genera una imagen sintética y solo registra los eventos de entrada.
package main

import (
	"image"
	"image/color"
	"log"
	"sync"
	"time"
)

const anchoPrueba, altoPrueba = 1280, 720

var (
	pruebaMu sync.Mutex
	cursorX  = anchoPrueba / 2
	cursorY  = altoPrueba / 2
)

func prepararSistema()  {}
func evitarSuspension() {}

func tamanoPantalla() (int, int) { return anchoPrueba, altoPrueba }

func capturar(escala int) (*image.RGBA, error) {
	w, h := anchoPrueba*escala/100, altoPrueba*escala/100
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fase := int(time.Now().UnixMilli()/40) % w
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{uint8(x * 255 / w), uint8(y * 255 / h), 120, 255}
			if x >= fase && x < fase+40 && y < 40 {
				c = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	pruebaMu.Lock()
	cx, cy := cursorX*escala/100, cursorY*escala/100
	pruebaMu.Unlock()
	for dy := -6; dy <= 6; dy++ {
		for dx := -6; dx <= 6; dx++ {
			img.SetRGBA(cx+dx, cy+dy, color.RGBA{0, 0, 0, 255})
		}
	}
	return img, nil
}

func moverMouse(x, y int) {
	pruebaMu.Lock()
	cursorX, cursorY = x, y
	pruebaMu.Unlock()
}

func botonMouse(b int, abajo bool) { registrar("boton", b, abajo) }
func ruedaMouse(d int)             { registrar("rueda", d) }
func tecla(k int, code string, abajo bool) {
	registrar("tecla", k, code, abajo)
}

func registrar(v ...any) {
	log.Println(append([]any{"[entrada]"}, v...)...)
}
