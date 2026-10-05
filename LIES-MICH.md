# SIGNL Download-Server

Ein kleiner Server für Filme und Serien, die du in SIGNL herunterladen willst —
**auf dem Server statt auf dem iPhone oder Apple TV**. Er läuft auf einem Mac,
Windows-PC, Linux-Rechner, NAS oder im Docker-Container und braucht keine
Installation weiterer Programme.

Der Server liefert keine Inhalte. Er lädt nur, was SIGNL ihm aus **deinen**
Quellen schickt, und speichert es in einem Ordner.

## Starten

| System  | Wie |
|---------|-----|
| macOS   | Im Terminal `./signl-server` (beim ersten Mal: Rechtsklick → Öffnen, falls macOS warnt). Autostart: `de.deltacode.signl-server.plist` |
| Windows | `start.bat` doppelklicken. Autostart: `autostart-einrichten.bat` |
| Linux   | `./signl-server`. Dauerhaft: `signl-server.service` (Anleitung in der Datei) |
| Docker  | `docker compose up -d` (oder `docker run -d -p 8484:8484 -v signl-data:/data signl-server`) |

Beim Start steht im Fenster bzw. Log:

```
Adresse:   http://192.168.1.20:8484
Schlüssel: 3f9c…
```

## In SIGNL verbinden

**Einstellungen → Download-Server → Server hinzufügen** → Adresse und Schlüssel
eintragen. Danach lässt sich bei jedem Download wählen, wohin er geht, oder in den
Einstellungen ein Standard festlegen („Dieses Gerät" / dein Server).

## Einstellungen (Umgebungsvariablen oder Optionen)

| Variable | Option | Standard | Bedeutung |
|----------|--------|----------|-----------|
| `SIGNL_PORT` | `-port` | 8484 | Port |
| `SIGNL_DATA` | `-data` | `~/SIGNL-Server` (Docker: `/data`) | Hier liegen die Filme (`media/`), der Schlüssel und der Zustand |
| `SIGNL_NAME` | `-name` | Rechnername | Anzeigename in SIGNL |
| `SIGNL_PARALLEL` | `-parallel` | 2 | Gleichzeitige Downloads |
| `SIGNL_TOKEN` | – | wird erzeugt | Eigener Zugangsschlüssel (mind. 16 Zeichen) |

`signl-server -show-token` zeigt den Schlüssel noch einmal.

## Ordner

```
media/Filme/Titel.mkv
media/Serien/Serienname/Titel - S1 · E3.mkv
```

## Sicherheit

* Jede Anfrage braucht den Schlüssel. Ohne ihn antwortet der Server nur mit „401".
* **Stell den Port nicht ungeschützt ins Internet.** Der Server spricht kein HTTPS.
  Für den Zugriff von unterwegs nimm ein VPN (z. B. WireGuard oder das eingebaute
  SIGNL VPN auf dem Router/PC) oder einen Reverse-Proxy mit HTTPS.
* Die Adressen deiner Anbieter (mit Zugangsdaten) gibt der Server nie wieder heraus.

## Streams (HLS)

Normale Dateien lädt der Server direkt. Listen mit der Endung `.m3u8` braucht
`ffmpeg` — im Docker-Image ist es enthalten, sonst bitte installieren
(`brew install ffmpeg`, `apt install ffmpeg`, `winget install ffmpeg`).

## Selbst bauen

Go ab 1.22: `./build.sh 1.0.0` erzeugt alle Pakete in `dist/`. Tests: `go test ./...`.

## Web-Oberfläche

`http://<Adresse>:8484/` zeigt die Downloads mit Fortschritt; Anmeldung mit dem Schlüssel.
