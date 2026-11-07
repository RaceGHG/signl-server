// SIGNL Download-Server
//
// Ein kleiner Server, auf den die SIGNL-App Filme und Serien laden kann —
// statt auf das iPhone oder den Apple TV. Nur Standardbibliothek, eine Datei
// pro Betriebssystem, läuft auch im Docker-Container.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Version wird beim Bauen gesetzt (-ldflags "-X main.Version=…").
var Version = "1.2.0"

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func main() {
	port := flag.Int("port", atoi(env("SIGNL_PORT", "8484")), "Port (SIGNL_PORT)")
	data := flag.String("data", env("SIGNL_DATA", defaultDataDir()), "Datenordner (SIGNL_DATA)")
	name := flag.String("name", env("SIGNL_NAME", hostname()), "Anzeigename (SIGNL_NAME)")
	parallel := flag.Int("parallel", atoi(env("SIGNL_PARALLEL", "2")), "Gleichzeitige Downloads (SIGNL_PARALLEL)")
	showToken := flag.Bool("show-token", false, "Zugangsschlüssel ausgeben und beenden")
	flag.Parse()

	if err := os.MkdirAll(filepath.Join(*data, "media"), 0o755); err != nil {
		log.Fatalf("Datenordner nicht beschreibbar: %v", err)
	}
	token := loadToken(*data)
	if *showToken {
		fmt.Println(token)
		return
	}

	store, err := newStore(*data)
	if err != nil {
		log.Fatalf("Zustand nicht lesbar: %v", err)
	}
	engine := newEngine(store, filepath.Join(*data, "media"), *parallel)
	engine.resume()

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(*port),
		Handler: newAPI(store, engine, token, *name, *data),
	}

	fmt.Printf("\nSIGNL Download-Server %s\n", Version)
	fmt.Printf("  Name:      %s\n", *name)
	fmt.Printf("  Daten:     %s\n", *data)
	for _, ip := range localIPs() {
		fmt.Printf("  Adresse:   http://%s:%d\n", ip, *port)
	}
	fmt.Printf("  Schlüssel: %s\n\n", token)
	fmt.Println("In SIGNL: Einstellungen → Download-Server → Server hinzufügen.")
	codeZeigen(*port, token, *name)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	engine.stopAll()
	_ = srv.Close()
}

// codeZeigen gibt den Einrichtungs-Code aus (SIGNL_QR=0 schaltet ihn ab). Adresse: SIGNL_ADDRESS, sonst die Adresse im Heimnetz.
func codeZeigen(port int, token, name string) {
	if os.Getenv("SIGNL_QR") == "0" {
		return
	}
	adresse := strings.TrimRight(strings.TrimSpace(os.Getenv("SIGNL_ADDRESS")), "/")
	if adresse == "" {
		if imDocker() {
			fmt.Println("\nQR-Code zum Einrichten: Setze SIGNL_ADDRESS auf die Adresse, unter der die Apps den Server erreichen")
			fmt.Println("(z. B. http://192.168.1.20:8484) — im Container kennt der Server nur seine interne Adresse.")
			return
		}
		ip := besteAdresse(localIPs())
		if ip == "" {
			return
		}
		adresse = fmt.Sprintf("http://%s:%d", ip, port)
	}
	link := einrichtungsLink(adresse, token, name)
	bild, err := qrTerminal(link)
	if err != nil {
		return
	}
	fmt.Println("\nOder in der App scannen (iPhone/iPad: Download-Server → Code scannen, Android: Download-Server → QR-Code scannen):")
	fmt.Println()
	fmt.Print(bild)
	fmt.Printf("\n  %s\n\n", link)
	fmt.Println("Der Code enthält den Zugangsschlüssel — zeig ihn nur deinen eigenen Geräten.")
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "SIGNL Server"
}

func defaultDataDir() string {
	if _, err := os.Stat("/data"); err == nil {
		return "/data" // Docker
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "SIGNL-Server")
	}
	return "./signl-data"
}

// loadToken liest den Zugangsschlüssel oder legt beim ersten Start einen an.
// SIGNL_TOKEN hat Vorrang.
func loadToken(dir string) string {
	if t := os.Getenv("SIGNL_TOKEN"); t != "" {
		return t
	}
	path := filepath.Join(dir, "token")
	if b, err := os.ReadFile(path); err == nil && len(b) >= 16 {
		return string(trimNL(b))
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		log.Fatal(err)
	}
	t := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		log.Fatalf("Schlüssel nicht speicherbar: %v", err)
	}
	return t
}

func trimNL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func localIPs() []string {
	var out []string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() && ipn.IP.To4() != nil {
			out = append(out, ipn.IP.String())
		}
	}
	return out
}
