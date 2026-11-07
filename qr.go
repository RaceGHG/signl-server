package main

import (
	"net/url"
	"os"
	"strings"

	"rsc.io/qr"
)

// Einrichtung per QR-Code: Der Server zeigt beim Start einen Code mit
//
//	signl://dlserver?u=<Adresse>&k=<Zugangsschlüssel>&n=<Name>
//
// Die SIGNL-Apps (iPhone, iPad, Android) scannen ihn und legen den Server an — wie das Eintippen von Adresse und Schlüssel.
// Der Code enthält den Schlüssel: Er gehört nur auf den eigenen Bildschirm.

// einrichtungsLink baut den Link für den QR-Code.
func einrichtungsLink(adresse, schluessel, name string) string {
	q := url.Values{}
	q.Set("u", adresse)
	q.Set("k", schluessel)
	if strings.TrimSpace(name) != "" {
		q.Set("n", name)
	}
	// Leerzeichen als %20 statt +: So lesen alle Apps den Namen gleich (Apple wertet + nicht als Leerzeichen).
	return "signl://dlserver?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

// qrTerminal zeichnet den Code mit Halbblöcken (▀ ▄ █): zwei Modulzeilen je Textzeile, schwarz auf weiß mit Ruhezone.
// Die Farben sind fest gesetzt (ANSI), damit der Code auch in dunklen Terminals lesbar bleibt.
func qrTerminal(text string) (string, error) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	const rand = 2
	n := c.Size + 2*rand
	dunkel := func(x, y int) bool {
		x -= rand
		y -= rand
		return x >= 0 && y >= 0 && x < c.Size && y < c.Size && c.Black(x, y)
	}
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		b.WriteString("  \x1b[30;47m")
		for x := 0; x < n; x++ {
			oben, unten := dunkel(x, y), y+1 < n && dunkel(x, y+1)
			switch {
			case oben && unten:
				b.WriteString("█")
			case oben:
				b.WriteString("▀")
			case unten:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

// imDocker: Im Container kennt der Server nur seine interne Adresse — dann braucht der Code SIGNL_ADDRESS.
func imDocker() bool {
	_, err := os.Stat("/.dockerenv")
	return err == nil
}

// besteAdresse wählt für den Code eine Adresse im Heimnetz (192.168.…, dann 10.…, dann 172.16–31.…).
func besteAdresse(ips []string) string {
	for _, praefix := range []string{"192.168.", "10."} {
		for _, ip := range ips {
			if strings.HasPrefix(ip, praefix) {
				return ip
			}
		}
	}
	for _, ip := range ips {
		if strings.HasPrefix(ip, "172.") {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}
