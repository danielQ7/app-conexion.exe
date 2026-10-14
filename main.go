// Conexión Remota: control remoto de PCs en red local (uso personal).
//
// Un solo .exe con dos modos:
//   - servidor: comparte esta PC (pantalla + mouse + teclado) por HTTP en el puerto 7070.
//   - panel:    abre en el navegador un panel para controlar hasta 2 PCs a la vez.
package main

import (
	"bufio"
	"context"
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

// Duración de las sesiones: se cierran tras un rato sin uso o, como máximo,
// tras unas horas aunque se sigan usando (hay que volver a poner la clave).
const (
	sesionInactiva = 30 * time.Minute
	sesionMaxima   = 12 * time.Hour
)

type sesion struct {
	ip     string
	creada time.Time
	ultima time.Time
}

type servidor struct {
	clave string

	mu       sync.Mutex
	tokens   map[string]*sesion // token -> sesión del cliente
	fallos   map[string]int     // IP -> intentos fallidos
	bloqueos map[string]time.Time

	entradaMu sync.Mutex // serializa el mouse/teclado entre los clientes
}

func iniciarServidor(puerto int, clave string) {
	s := &servidor{
		clave:    clave,
		tokens:   map[string]*sesion{},
		fallos:   map[string]int{},
		bloqueos: map[string]time.Time{},
	}
	estatico, _ := fs.Sub(webFS, "web")
	evitarSuspension()
	go s.limpiarSesiones()

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
	ipsTS := ipsTailscale()
	for _, ip := range ipsTS {
		fmt.Printf(" INTERNET (Tailscale): %s  ->  usá esta IP en la otra PC\n", ip)
	}
	for _, ip := range ipsLocales() {
		fmt.Printf(" Red local: %s   ->  http://%s:%d\n", ip, ip, puerto)
	}
	if len(ipsTS) == 0 {
		fmt.Println(" (Para controlar por INTERNET instalá Tailscale en las 2 PCs; ver README)")
	}
	fmt.Printf(" Clave:    %s\n", clave)
	fmt.Println(" Cerrá esta ventana para dejar de compartir.")
	fmt.Println("------------------------------------------")

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", puerto),
		Handler:           cabecerasSeguras(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
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
		registrar("BLOQUEADO: intento de acceso desde %s mientras estaba bloqueada", ip)
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
		bloqueada := s.fallos[ip] >= 5
		if bloqueada {
			s.bloqueos[ip] = time.Now().Add(time.Minute)
			s.fallos[ip] = 0
		}
		s.mu.Unlock()
		registrar("Clave incorrecta desde %s", ip)
		if bloqueada {
			registrar("ALERTA: %s bloqueada 1 minuto por 5 claves incorrectas (posible intento de intrusión)", ip)
		}
		time.Sleep(500 * time.Millisecond)
		http.Error(w, "Clave incorrecta", http.StatusUnauthorized)
		return
	}

	token := tokenAleatorio()
	ahora := time.Now()
	s.mu.Lock()
	s.tokens[token] = &sesion{ip: ip, creada: ahora, ultima: ahora}
	delete(s.fallos, ip)
	s.mu.Unlock()
	registrar("CONECTADO: %s está controlando esta PC", ip)

	ancho, alto := tamanoPantalla()
	nombre, _ := os.Hostname()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"token": token, "nombre": nombre, "ancho": ancho, "alto": alto,
	})
}

func (s *servidor) autorizado(r *http.Request) bool {
	token := tokenDe(r)
	if token == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.tokens[token]
	if !ok {
		return false
	}
	// El token solo vale desde la IP que hizo login: si se filtra, no sirve en otra PC.
	if ses.ip != ipCliente(r) {
		registrar("ALERTA: token usado desde %s, pero pertenece a %s; acceso rechazado", ipCliente(r), ses.ip)
		return false
	}
	if s.vencida(ses, time.Now()) {
		delete(s.tokens, token)
		registrar("Sesión de %s vencida", ses.ip)
		return false
	}
	ses.ultima = time.Now()
	return true
}

func tokenDe(r *http.Request) string {
	if t := r.Header.Get("X-Token"); t != "" {
		return t
	}
	return r.URL.Query().Get("t") // el <img> del stream no puede mandar cabeceras
}

func (s *servidor) vencida(ses *sesion, ahora time.Time) bool {
	return ahora.Sub(ses.ultima) > sesionInactiva || ahora.Sub(ses.creada) > sesionMaxima
}

// mantenerViva renueva la sesión mientras se está mirando la pantalla.
// Devuelve false si el token ya no existe o venció por tiempo máximo.
func (s *servidor) mantenerViva(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, ok := s.tokens[token]
	if !ok || s.vencida(ses, time.Now()) {
		return false
	}
	ses.ultima = time.Now()
	return true
}

// limpiarSesiones borra periódicamente las sesiones vencidas y los bloqueos viejos.
func (s *servidor) limpiarSesiones() {
	for range time.Tick(time.Minute) {
		ahora := time.Now()
		s.mu.Lock()
		for t, ses := range s.tokens {
			if s.vencida(ses, ahora) {
				delete(s.tokens, t)
				registrar("Sesión de %s vencida", ses.ip)
			}
		}
		for ip, hasta := range s.bloqueos {
			if ahora.After(hasta) {
				delete(s.bloqueos, ip)
			}
		}
		s.mu.Unlock()
	}
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

	// Corta la transmisión si la sesión vence (o se cerró) mientras se mira.
	ctx, cancelar := context.WithCancel(r.Context())
	defer cancelar()
	token := tokenDe(r)
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if !s.mantenerViva(token) {
					cancelar()
					return
				}
			}
		}
	}()

	enviarStream(ctx, w, flusher, calidad, escala, fps)
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
	http.Serve(ln, cabecerasPanel(mux))
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

// ---------------------------------------------------------------- seguridad

// cabecerasSeguras agrega cabeceras HTTP de protección a todas las respuestas.
// El visor solo se puede mostrar dentro del panel local (127.0.0.1 / localhost),
// así ninguna página de internet puede meterlo en un iframe para engañar al usuario.
func cabecerasSeguras(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := w.Header()
		c.Set("Content-Security-Policy", "default-src 'self'; img-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'self' http://127.0.0.1:* http://localhost:*")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

// cabecerasPanel protege el panel local: no se puede meter en un iframe y solo
// responde si se lo pide con 127.0.0.1/localhost (evita ataques de "DNS rebinding").
func cabecerasPanel(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "acceso no permitido", http.StatusForbidden)
			return
		}
		c := w.Header()
		c.Set("Content-Security-Policy", "frame-ancestors 'none'")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "no-referrer")
		h.ServeHTTP(w, r)
	})
}

var (
	registroMu  sync.Mutex
	registroLog *log.Logger
)

// registrar muestra el evento en la ventana y lo guarda en accesos.log (junto al
// .exe), para poder revisar después quién se conectó o intentó entrar.
func registrar(formato string, args ...any) {
	msg := fmt.Sprintf(formato, args...)
	log.Print(msg)
	registroMu.Lock()
	defer registroMu.Unlock()
	if registroLog == nil {
		exe, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.OpenFile(filepath.Join(filepath.Dir(exe), "accesos.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		registroLog = log.New(f, "", log.LstdFlags)
	}
	registroLog.Print(msg)
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

// ipsTailscale devuelve las IPs del rango de Tailscale (100.64.0.0/10),
// que son las que sirven para controlar la PC por internet.
func ipsTailscale() []string {
	_, red, _ := net.ParseCIDR("100.64.0.0/10")
	var ips []string
	ifaces, _ := net.Interfaces()
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok {
				if ip4 := ipn.IP.To4(); ip4 != nil && red.Contains(ip4) {
					ips = append(ips, ip4.String())
				}
			}
		}
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
