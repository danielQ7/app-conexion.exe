//go:build windows

// Captura de pantalla (GDI) y simulación de mouse/teclado (user32) usando
// solo llamadas al sistema: no hace falta instalar nada en Windows.
package main

import (
	"errors"
	"image"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pGetDC                  = user32.NewProc("GetDC")
	pReleaseDC              = user32.NewProc("ReleaseDC")
	pGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	pSetProcessDPIAware     = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwareCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetCursorPos           = user32.NewProc("SetCursorPos")
	pMouseEvent             = user32.NewProc("mouse_event")
	pKeybdEvent             = user32.NewProc("keybd_event")
	pMapVirtualKeyW         = user32.NewProc("MapVirtualKeyW")
	pGetCursorInfo          = user32.NewProc("GetCursorInfo")
	pGetIconInfo            = user32.NewProc("GetIconInfo")
	pDrawIconEx             = user32.NewProc("DrawIconEx")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pStretchBlt             = gdi32.NewProc("StretchBlt")
	pSetStretchBltMode      = gdi32.NewProc("SetStretchBltMode")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pSetThreadExecState     = kernel32.NewProc("SetThreadExecutionState")
)

const (
	smCxScreen = 0
	smCyScreen = 1
	srcCopy    = 0x00CC0020
	halftone   = 4
	diNormal   = 3

	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800

	keyExtended = 0x0001
	keyUp       = 0x0002
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [4]uint32
}

type punto struct{ X, Y int32 }

type cursorInfo struct {
	Size     uint32
	Flags    uint32
	Cursor   uintptr
	Posicion punto
}

type iconInfo struct {
	EsIcono  int32
	HotspotX uint32
	HotspotY uint32
	Mascara  uintptr
	Color    uintptr
}

func prepararSistema() {
	// Para capturar la resolución real aunque Windows tenga escala 125%/150%.
	// -4 = DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	if pSetProcessDpiAwareCtx.Find() == nil {
		if r, _, _ := pSetProcessDpiAwareCtx.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	pSetProcessDPIAware.Call()
}

func evitarSuspension() {
	// ES_CONTINUOUS | ES_SYSTEM_REQUIRED | ES_DISPLAY_REQUIRED
	pSetThreadExecState.Call(0x80000000 | 0x00000001 | 0x00000002)
}

func tamanoPantalla() (int, int) {
	w, _, _ := pGetSystemMetrics.Call(smCxScreen)
	h, _, _ := pGetSystemMetrics.Call(smCyScreen)
	return int(int32(w)), int(int32(h))
}

func capturar(escala int) (*image.RGBA, error) {
	sw, sh := tamanoPantalla()
	if sw <= 0 || sh <= 0 {
		return nil, errors.New("pantalla no disponible")
	}
	dw, dh := sw*escala/100, sh*escala/100
	if dw < 1 || dh < 1 {
		dw, dh = sw, sh
	}

	hdcPantalla, _, _ := pGetDC.Call(0)
	if hdcPantalla == 0 {
		return nil, errors.New("GetDC falló")
	}
	defer pReleaseDC.Call(0, hdcPantalla)

	hdcMem, _, _ := pCreateCompatibleDC.Call(hdcPantalla)
	if hdcMem == 0 {
		return nil, errors.New("CreateCompatibleDC falló")
	}
	defer pDeleteDC.Call(hdcMem)

	hbm, _, _ := pCreateCompatibleBitmap.Call(hdcPantalla, uintptr(dw), uintptr(dh))
	if hbm == 0 {
		return nil, errors.New("CreateCompatibleBitmap falló")
	}
	defer pDeleteObject.Call(hbm)

	viejo, _, _ := pSelectObject.Call(hdcMem, hbm)
	var ok uintptr
	if dw == sw && dh == sh {
		ok, _, _ = pBitBlt.Call(hdcMem, 0, 0, uintptr(dw), uintptr(dh), hdcPantalla, 0, 0, srcCopy)
	} else {
		pSetStretchBltMode.Call(hdcMem, halftone)
		ok, _, _ = pStretchBlt.Call(hdcMem, 0, 0, uintptr(dw), uintptr(dh), hdcPantalla, 0, 0, uintptr(sw), uintptr(sh), srcCopy)
	}
	if ok == 0 {
		pSelectObject.Call(hdcMem, viejo)
		return nil, errors.New("BitBlt falló (¿pantalla bloqueada?)")
	}
	dibujarCursor(hdcMem, sw, sh, dw, dh)
	pSelectObject.Call(hdcMem, viejo)

	var bi bitmapInfo
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(dw)
	bi.Header.Height = -int32(dh) // negativo = filas de arriba hacia abajo
	bi.Header.Planes = 1
	bi.Header.BitCount = 32

	img := image.NewRGBA(image.Rect(0, 0, dw, dh))
	lineas, _, _ := pGetDIBits.Call(hdcMem, hbm, 0, uintptr(dh),
		uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&bi)), 0)
	if lineas == 0 {
		return nil, errors.New("GetDIBits falló")
	}
	// BGRA -> RGBA
	p := img.Pix
	for i := 0; i+3 < len(p); i += 4 {
		p[i], p[i+2], p[i+3] = p[i+2], p[i], 255
	}
	return img, nil
}

func dibujarCursor(hdc uintptr, sw, sh, dw, dh int) {
	var ci cursorInfo
	ci.Size = uint32(unsafe.Sizeof(ci))
	if r, _, _ := pGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.Flags&1 == 0 || ci.Cursor == 0 {
		return
	}
	var ii iconInfo
	if r, _, _ := pGetIconInfo.Call(ci.Cursor, uintptr(unsafe.Pointer(&ii))); r != 0 {
		if ii.Mascara != 0 {
			pDeleteObject.Call(ii.Mascara)
		}
		if ii.Color != 0 {
			pDeleteObject.Call(ii.Color)
		}
	}
	x := (int(ci.Posicion.X) - int(ii.HotspotX)) * dw / sw
	y := (int(ci.Posicion.Y) - int(ii.HotspotY)) * dh / sh
	pDrawIconEx.Call(hdc, uintptr(int64(x)), uintptr(int64(y)), ci.Cursor, 0, 0, 0, 0, diNormal)
}

func moverMouse(x, y int) {
	pSetCursorPos.Call(uintptr(int64(x)), uintptr(int64(y)))
}

func botonMouse(b int, abajo bool) {
	var f uintptr
	switch b {
	case 0:
		f = mouseLeftUp
		if abajo {
			f = mouseLeftDown
		}
	case 1:
		f = mouseMiddleUp
		if abajo {
			f = mouseMiddleDown
		}
	case 2:
		f = mouseRightUp
		if abajo {
			f = mouseRightDown
		}
	default:
		return
	}
	pMouseEvent.Call(f, 0, 0, 0, 0)
}

func ruedaMouse(d int) {
	pMouseEvent.Call(mouseWheel, 0, 0, uintptr(int64(d)), 0)
}

// Teclas que Windows marca como "extendidas".
var teclasExtendidas = map[int]bool{
	0x21: true, 0x22: true, 0x23: true, 0x24: true, // RePág AvPág Fin Inicio
	0x25: true, 0x26: true, 0x27: true, 0x28: true, // flechas
	0x2D: true, 0x2E: true, // Insert Supr
	0x5B: true, 0x5C: true, 0x5D: true, // Windows izq/der, menú
	0x6F: true, 0x90: true, 0x2C: true, // / numérico, BloqNum, ImprPant
}

func tecla(k int, code string, abajo bool) {
	if k <= 0 || k > 254 {
		return
	}
	vk := k
	ext := teclasExtendidas[vk]
	switch code {
	case "ControlRight", "AltRight":
		ext = true
	case "NumpadEnter":
		ext = true
	}
	scan, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	var f uintptr
	if ext {
		f |= keyExtended
	}
	if !abajo {
		f |= keyUp
	}
	pKeybdEvent.Call(uintptr(vk), scan&0xFF, f, 0)
}
