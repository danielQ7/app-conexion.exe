// Conexión Remota: control remoto de PCs en red local (uso personal).
//
// Un solo .exe con dos modos:
//   - servidor: comparte esta PC (pantalla + mouse + teclado) por HTTP en el puerto 7070.
//   - panel:    abre en el navegador un panel para controlar hasta 2 PCs a la vez.
package main

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

const maxPCs = 2

func main() {
	modo := flag.String("modo", "", "servidor | panel | ambos (si se omite, muestra un menú)")
	puerto := flag.Int("puerto", 7070, "puerto del servidor (PC controlada)")
	puertoPanel := flag.Int("puerto-panel", 7071, "puerto local del panel de control")
	clave := flag.String("clave", "", "clave de acceso fija (si se omite: clave.txt junto al .exe, o una aleatoria)")
	flag.Parse()

	prepararSistema()

	if *modo == "" {
		*modo = menu()
	}

	var wg sync.WaitGroup
	switch *modo {
	case "servidor":
		wg.Add(1)
		go func() { defer wg.Done(); iniciarServidor(*puerto, obtenerClave(*clave)) }()
	case "panel":
		wg.Add(1)
		go func() { defer wg.Done(); iniciarPanel(*puertoPanel) }()
	case "ambos":
		wg.Add(2)
		go func() { defer wg.Done(); iniciarServidor(*puerto, obtenerClave(*clave)) }()
		go func() { defer wg.Done(); iniciarPanel(*puertoPanel) }()
	default:
		fmt.Println("Modo desconocido:", *modo)
		esperarEnter()
		return
	}
	wg.Wait()
	esperarEnter()
}

func menu() string {
	fmt.Println("==========================================")
	fmt.Println("        CONEXIÓN REMOTA  (uso personal)")
	fmt.Println("==========================================")
	fmt.Println(" 1) Permitir que controlen ESTA PC")
	fmt.Println(" 2) Controlar otras PCs (hasta 2)")
	fmt.Println(" 3) Las dos cosas")
	fmt.Print("\nElegí una opción [1/2/3]: ")
	lector := bufio.NewReader(os.Stdin)
	for {
		linea, err := lector.ReadString('\n')
		if err != nil {
			return "servidor"
		}
		switch strings.TrimSpace(linea) {
		case "1":
			return "servidor"
		case "2":
			return "panel"
		case "3":
			return "ambos"
		}
		fmt.Print("Opción inválida. Elegí 1, 2 o 3: ")
	}
}

func esperarEnter() {
	fmt.Print("\nPresioná Enter para salir...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

// ---------------------------------------------------------------- clave

func obtenerClave(flagClave string) string {
	if flagClave != "" {
		return flagClave
	}
	if exe, err := os.Executable(); err == nil {
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "clave.txt")); err == nil {
			if c := strings.TrimSpace(string(b)); c != "" {
				return c
			}
		}
	}
	return claveAleatoria(8)
}

func claveAleatoria(n int) string {
	const letras = "abcdefghjkmnpqrstuvwxyz23456789" // sin caracteres confusos (l/1, o/0)
	var sb strings.Builder
	for i := 0; i < n; i++ {
		k, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letras))))
		sb.WriteByte(letras[k.Int64()])
	}
	return sb.String()
}

func tokenAleatorio() string {
	b := make([]byte, 24)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- servidor (PC controlada)

type servidor struct {
	clave string

	mu       sync.Mutex
	tokens   map[string]string // token -> IP del cliente
	fallos   map[string]int    // IP -> intentos fallidos
	bloqueos map[string]time.Time

	entradaMu sync.Mutex // serializa el mouse/teclado entre los clientes
}

func iniciarServidor(puerto int, clave string) {
	s := &servidor{
		clave:    clave,
		tokens:   map[string]string{},
		fallos:   map[string]int{},
		bloqueos: map[string]time.Time{},
	}
	estatico, _ := fs.Sub(webFS, "web")
	evitarSuspension()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		servirArchivo(w, estatico, "visor.html")
	})
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/stream", s.stream)
	mux.HandleFunc("POST /api/entrada", s.entrada)

	ancho, alto := tamanoPantalla()
	nombre, _ := os.Hostname()
	fmt.Println()
	fmt.Println("------------------------------------------")
	fmt.Println(" ESTA PC SE PUEDE CONTROLAR")
	fmt.Println("------------------------------------------")
	fmt.Printf(" Nombre:   %s  (pantalla %dx%d)\n", nombre, ancho, alto)
	for _, ip := range ipsLocales() {
		fmt.Printf(" IP:       %s   ->  http://%s:%d\n", ip, ip, puerto)
	}
	fmt.Printf(" Clave:    %s\n", clave)
	fmt.Println(" Cerrá esta ventana para dejar de compartir.")
	fmt.Println("------------------------------------------")

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", puerto),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Println("ERROR al iniciar el servidor:", err)
	}
}

func ipCliente(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *servidor) login(w http.ResponseWriter, r *http.Request) {
	ip := ipCliente(r)
	s.mu.Lock()
	if hasta, ok := s.bloqueos[ip]; ok && time.Now().Before(hasta) {
		s.mu.Unlock()
		http.Error(w, "Demasiados intentos. Esperá un minuto.", http.StatusTooManyRequests)
		return
	}
	s.mu.Unlock()

	var datos struct {
		Clave string `json:"clave"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&datos); err != nil {
		http.Error(w, "solicitud inválida", http.StatusBadRequest)
		return
	}
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(datos.Clave)), []byte(s.clave)) != 1 {
		s.mu.Lock()
		s.fallos[ip]++
		if s.fallos[ip] >= 5 {
			s.bloqueos[ip] = time.Now().Add(time.Minute)
			s.fallos[ip] = 0
		}
		s.mu.Unlock()
		log.Printf("Clave incorrecta desde %s", ip)
		time.Sleep(500 * time.Millisecond)
		http.Error(w, "Clave incorrecta", http.StatusUnauthorized)
		return
	}

	token := tokenAleatorio()
	s.mu.Lock()
	s.tokens[token] = ip
	delete(s.fallos, ip)
	s.mu.Unlock()
	log.Printf("CONECTADO: %s está controlando esta PC", ip)

	ancho, alto := tamanoPantalla()
	nombre, _ := os.Hostname()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token": token, "nombre": nombre, "ancho": ancho, "alto": alto,
	})
}

func (s *servidor) autorizado(r *http.Request) bool {
	token := r.Header.Get("X-Token")
	if token == "" {
		token = r.URL.Query().Get("t")
	}
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.tokens[token]
	return ok
}

func (s *servidor) stream(w http.ResponseWriter, r *http.Request) {
	if !s.autorizado(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	calidad := limitar(atoi(q.Get("calidad"), 60), 10, 95)
	escala := limitar(atoi(q.Get("escala"), 100), 25, 100)
	fps := limitar(atoi(q.Get("fps"), 12), 1, 30)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming no soportado", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "close")

	enviarStream(r.Context(), w, flusher, calidad, escala, fps)
}

type evento struct {
	T string  `json:"t"` // m=mover d=botón abajo u=botón arriba w=rueda kd=tecla abajo ku=tecla arriba
	X float64 `json:"x"` // 0..1
	Y float64 `json:"y"` // 0..1
	B int     `json:"b"` // botón: 0 izq, 1 medio, 2 der
	D int     `json:"d"` // rueda
	K int     `json:"k"` // código de tecla (keyCode)
	C string  `json:"c"` // code del navegador (KeyA, ControlRight...)
}

func (s *servidor) entrada(w http.ResponseWriter, r *http.Request) {
	if !s.autorizado(r) {
		http.Error(w, "no autorizado", http.StatusUnauthorized)
		return
	}
	var eventos []evento
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&eventos); err != nil {
		http.Error(w, "solicitud inválida", http.StatusBadRequest)
		return
	}
	s.entradaMu.Lock()
	defer s.entradaMu.Unlock()
	ancho, alto := tamanoPantalla()
	for _, e := range eventos {
		switch e.T {
		case "m":
			moverMouse(int(e.X*float64(ancho-1)+0.5), int(e.Y*float64(alto-1)+0.5))
		case "d", "u":
			moverMouse(int(e.X*float64(ancho-1)+0.5), int(e.Y*float64(alto-1)+0.5))
			botonMouse(e.B, e.T == "d")
		case "w":
			ruedaMouse(e.D)
		case "kd", "ku":
			tecla(e.K, e.C, e.T == "kd")
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- panel (PC que controla)

func iniciarPanel(puerto int) {
	estatico, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		servirArchivo(w, estatico, "panel.html")
	})
	// Solo escucha en esta PC: el panel no se expone a la red.
	dir := fmt.Sprintf("127.0.0.1:%d", puerto)
	ln, err := net.Listen("tcp", dir)
	if err != nil {
		fmt.Println("ERROR al abrir el panel:", err)
		return
	}
	url := "http://" + dir + "/"
	fmt.Println()
	fmt.Println("------------------------------------------")
	fmt.Printf(" PANEL DE CONTROL (hasta %d PCs): %s\n", maxPCs, url)
	fmt.Println(" Si no se abrió solo, copiá esa dirección en el navegador.")
	fmt.Println("------------------------------------------")
	abrirNavegador(url)
	http.Serve(ln, mux)
}

func servirArchivo(w http.ResponseWriter, f fs.FS, nombre string) {
	b, err := fs.ReadFile(f, nombre)
	if err != nil {
		http.Error(w, "no encontrado", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func abrirNavegador(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// ---------------------------------------------------------------- utilidades

func ipsLocales() []string {
	var ips []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && !ip4.IsLinkLocalUnicast() {
					ips = append(ips, ip4.String())
				}
			}
		}
	}
	if len(ips) == 0 {
		ips = append(ips, "127.0.0.1")
	}
	return ips
}

func atoi(s string, def int) int {
	var n int
	if _, err := fmt.Sscan(s, &n); err != nil {
		return def
	}
	return n
}

func limitar(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
