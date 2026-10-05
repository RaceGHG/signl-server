package main

import (
	"crypto/subtle"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

type api struct {
	store *store
	eng   *engine
	token string
	name  string
	data  string
	mux   *http.ServeMux
}

func newAPI(s *store, e *engine, token, name, data string) http.Handler {
	a := &api{store: s, eng: e, token: token, name: name, data: data, mux: http.NewServeMux()}
	a.mux.HandleFunc("GET /{$}", a.index)
	a.mux.HandleFunc("GET /api/v1/info", a.auth(a.info))
	a.mux.HandleFunc("GET /api/v1/downloads", a.auth(a.list))
	a.mux.HandleFunc("POST /api/v1/downloads", a.auth(a.add))
	a.mux.HandleFunc("POST /api/v1/downloads/{id}/cancel", a.auth(a.cancel))
	a.mux.HandleFunc("POST /api/v1/downloads/{id}/retry", a.auth(a.retry))
	a.mux.HandleFunc("DELETE /api/v1/downloads/{id}", a.auth(a.remove))
	a.mux.HandleFunc("GET /api/v1/files/{id}", a.auth(a.file))
	return a.mux
}

// auth prüft den Zugangsschlüssel: Kopfzeile `Authorization: Bearer …` oder,
// für Player, die keine Kopfzeilen setzen können, `?token=…`.
func (a *api) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(a.token)) != 1 {
			// Wer falsch rät, wartet kurz.
			time.Sleep(400 * time.Millisecond)
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Zugangsschlüssel falsch"})
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *api) index(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (a *api) info(w http.ResponseWriter, r *http.Request) {
	free, total := diskSpace(a.data)
	writeJSON(w, 200, map[string]any{
		"name":       a.name,
		"version":    Version,
		"api":        1,
		"ffmpeg":     hasFFmpeg(),
		"freeBytes":  free,
		"totalBytes": total,
	})
}

func (a *api) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"downloads": a.store.list()})
}

type addRequest struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Subtitle string  `json:"subtitle"`
	URL      string  `json:"url"`
	Poster   string  `json:"poster"`
	Kind     string  `json:"kind"`
	Folder   string  `json:"folder"`
	Duration float64 `json:"duration"`
}

func (a *api) add(w http.ResponseWriter, r *http.Request) {
	var req addRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, 400, map[string]string{"error": "Ungültige Anfrage"})
		return
	}
	u, err := url.Parse(req.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		writeJSON(w, 400, map[string]string{"error": "Die Adresse muss mit http:// oder https:// beginnen"})
		return
	}
	req.ID = strings.TrimSpace(req.ID)
	if req.ID == "" {
		req.ID = req.URL
	}
	if len(req.ID) > 300 {
		req.ID = req.ID[:300]
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = req.ID
	}
	switch req.Kind {
	case "movie", "series", "episode":
	default:
		req.Kind = "other"
	}

	if old, ok := a.store.get(req.ID); ok {
		switch old.State {
		case stateQueued, stateDownloading, stateCompleted:
			writeJSON(w, 200, old.public()) // schon da — nichts doppelt laden
			return
		}
	}
	d := Download{
		ID: req.ID, Title: req.Title, Subtitle: req.Subtitle, Poster: req.Poster,
		Kind: req.Kind, Folder: req.Folder, Duration: req.Duration,
		State: stateQueued, AddedAt: time.Now(), SourceURL: req.URL,
	}
	a.store.put(d)
	a.eng.start(d.ID)
	writeJSON(w, 202, d.public())
}

func (a *api) cancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := a.store.get(id); !ok {
		writeJSON(w, 404, map[string]string{"error": "Unbekannter Download"})
		return
	}
	a.eng.cancel(id)
	writeJSON(w, 200, map[string]string{"ok": "cancelled"})
}

func (a *api) retry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, ok := a.store.get(id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "Unbekannter Download"})
		return
	}
	if d.State == stateCompleted || d.State == stateDownloading || d.State == stateQueued {
		writeJSON(w, 200, d.public())
		return
	}
	a.store.update(id, true, func(x *Download) { x.Progress = 0; x.Bytes = 0 })
	a.eng.start(id)
	d, _ = a.store.get(id)
	writeJSON(w, 202, d.public())
}

func (a *api) remove(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, ok := a.store.get(id)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "Unbekannter Download"})
		return
	}
	a.eng.cancel(id)
	if d.File != "" {
		full := filepath.Join(a.eng.mediaDir, filepath.FromSlash(d.File))
		if within(a.eng.mediaDir, full) {
			_ = os.Remove(full)
			// Leere Ordner (Serie ohne Folgen) aufräumen, Basisordner bleibt.
			dir := filepath.Dir(full)
			for dir != filepath.Clean(a.eng.mediaDir) && within(a.eng.mediaDir, dir) {
				if os.Remove(dir) != nil {
					break
				}
				dir = filepath.Dir(dir)
			}
		}
	}
	a.store.remove(id)
	writeJSON(w, 200, map[string]string{"ok": "removed"})
}

// file liefert die fertige Datei mit Range-Unterstützung — zum Abspielen und
// zum Herunterladen.
func (a *api) file(w http.ResponseWriter, r *http.Request) {
	d, ok := a.store.get(r.PathValue("id"))
	if !ok || d.State != stateCompleted || d.File == "" {
		writeJSON(w, 404, map[string]string{"error": "Datei nicht vorhanden"})
		return
	}
	full := filepath.Join(a.eng.mediaDir, filepath.FromSlash(d.File))
	if !within(a.eng.mediaDir, full) {
		writeJSON(w, 404, map[string]string{"error": "Datei nicht vorhanden"})
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "Datei nicht vorhanden"})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Datei nicht lesbar"})
		return
	}
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}

// within: liegt p innerhalb von base (gegen ../-Tricks)?
func within(base, p string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
