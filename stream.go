package main

import (
	"bytes"
	"context"
	"fmt"
	"image/jpeg"
	"io"
	"net/http"
	"time"
)

// enviarStream manda la pantalla como MJPEG (multipart/x-mixed-replace),
// que el navegador muestra directamente en un <img>.
func enviarStream(ctx context.Context, w io.Writer, f http.Flusher, calidad, escala, fps int) {
	intervalo := time.Second / time.Duration(fps)
	var anterior []byte
	var buf bytes.Buffer
	repetidos := 0
	ultimoEnvio := time.Time{}

	for {
		inicio := time.Now()
		select {
		case <-ctx.Done():
			return
		default:
		}

		img, err := capturar(escala)
		if err != nil {
			// p.ej. pantalla bloqueada / UAC: reintentar
			time.Sleep(500 * time.Millisecond)
			continue
		}

		igual := anterior != nil && bytes.Equal(anterior, img.Pix)
		if igual {
			repetidos++
		} else {
			repetidos = 0
			anterior = append(anterior[:0], img.Pix...)
		}
		// Si nada cambió, no se reenvía; salvo una vez (algunos navegadores muestran
		// el frame anterior al último recibido) y un "latido" cada 2 s.
		if !igual || repetidos == 1 || time.Since(ultimoEnvio) > 2*time.Second {
			buf.Reset()
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: calidad}); err != nil {
				return
			}
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", buf.Len()); err != nil {
				return
			}
			if _, err := w.Write(buf.Bytes()); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return
			}
			f.Flush()
			ultimoEnvio = time.Now()
		}

		if d := intervalo - time.Since(inicio); d > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
		}
	}
}
