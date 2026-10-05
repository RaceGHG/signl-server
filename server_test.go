package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testServer startet API + Engine in einem Temp-Ordner.
func testServer(t *testing.T) (*httptest.Server, *engine, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := newStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := newEngine(s, filepath.Join(dir, "media"), 2)
	srv := httptest.NewServer(newAPI(s, e, "geheim123456789012", "Test", dir))
	t.Cleanup(srv.Close)
	return srv, e, dir
}

func call(t *testing.T, method, url, token string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func warte(t *testing.T, srv *httptest.Server, id, ziel string) Download {
	t.Helper()
	for i := 0; i < 100; i++ {
		_, b := call(t, "GET", srv.URL+"/api/v1/downloads", "geheim123456789012", nil)
		var l struct{ Downloads []Download }
		_ = json.Unmarshal(b, &l)
		for _, d := range l.Downloads {
			if d.ID == id && d.State == ziel {
				return d
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("Download %s erreicht %s nicht", id, ziel)
	return Download{}
}

func TestOhneSchluesselKeinZugriff(t *testing.T) {
	srv, _, _ := testServer(t)
	for _, tok := range []string{"", "falsch"} {
		if code, _ := call(t, "GET", srv.URL+"/api/v1/downloads", tok, nil); code != 401 {
			t.Fatalf("Token %q: %d statt 401", tok, code)
		}
	}
	if code, _ := call(t, "GET", srv.URL+"/api/v1/info?token=geheim123456789012", "", nil); code != 200 {
		t.Fatalf("?token= wird nicht akzeptiert: %d", code)
	}
}

func TestDownloadLaedtUndLiefertDatei(t *testing.T) {
	inhalt := bytes.Repeat([]byte("SIGNL"), 100_000)
	quelle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/x-matroska")
		http.ServeContent(w, r, "x.mkv", time.Now(), bytes.NewReader(inhalt))
	}))
	defer quelle.Close()

	srv, _, dir := testServer(t)
	code, _ := call(t, "POST", srv.URL+"/api/v1/downloads", "geheim123456789012", map[string]any{
		"id": "film1", "title": "Ein: Film/Test", "kind": "movie", "url": quelle.URL + "/video",
	})
	if code != 202 {
		t.Fatalf("POST: %d", code)
	}
	d := warte(t, srv, "film1", stateCompleted)
	if d.File != "Filme/Ein_ Film_Test.mkv" {
		t.Fatalf("Dateiname/Ordner: %q", d.File)
	}
	if st, err := os.Stat(filepath.Join(dir, "media", filepath.FromSlash(d.File))); err != nil || st.Size() != int64(len(inhalt)) {
		t.Fatalf("Datei fehlt oder falsche Größe: %v", err)
	}
	// Die Quelladresse darf nie nach außen.
	_, b := call(t, "GET", srv.URL+"/api/v1/downloads", "geheim123456789012", nil)
	if strings.Contains(string(b), quelle.URL) {
		t.Fatal("Quelladresse in der Antwort")
	}

	// Datei mit Range-Anfrage (wie ein Player).
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/files/film1?token=geheim123456789012", nil)
	req.Header.Set("Range", "bytes=5-9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || string(got) != "SIGNL" {
		t.Fatalf("Range: %d %q", resp.StatusCode, got)
	}

	// Doppelt anfordern lädt nicht noch einmal.
	if code, _ := call(t, "POST", srv.URL+"/api/v1/downloads", "geheim123456789012", map[string]any{
		"id": "film1", "title": "x", "url": quelle.URL,
	}); code != 200 {
		t.Fatalf("zweites POST: %d", code)
	}

	// Entfernen löscht Datei und leeren Ordner.
	if code, _ := call(t, "DELETE", srv.URL+"/api/v1/downloads/film1", "geheim123456789012", nil); code != 200 {
		t.Fatalf("DELETE: %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "media", "Filme")); !os.IsNotExist(err) {
		t.Fatal("leerer Ordner bleibt liegen")
	}
}

func TestFortsetzenNachUnterbrechung(t *testing.T) {
	inhalt := bytes.Repeat([]byte("0123456789"), 50_000)
	var range1 string
	quelle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		range1 = r.Header.Get("Range")
		http.ServeContent(w, r, "x.mp4", time.Now(), bytes.NewReader(inhalt))
	}))
	defer quelle.Close()

	srv, e, _ := testServer(t)
	// Eine halbe Teildatei liegt schon da.
	part := e.partPath("teil")
	_ = os.MkdirAll(filepath.Dir(part), 0o755)
	_ = os.WriteFile(part, inhalt[:200_000], 0o644)

	call(t, "POST", srv.URL+"/api/v1/downloads", "geheim123456789012", map[string]any{
		"id": "teil", "title": "Teil", "kind": "movie", "url": quelle.URL + "/a.mp4",
	})
	d := warte(t, srv, "teil", stateCompleted)
	if range1 != "bytes=200000-" {
		t.Fatalf("Range-Kopfzeile: %q", range1)
	}
	b, _ := os.ReadFile(filepath.Join(e.mediaDir, filepath.FromSlash(d.File)))
	if !bytes.Equal(b, inhalt) {
		t.Fatal("Datei nach dem Fortsetzen nicht identisch")
	}
}

func TestFehlerBeiNichtVideo(t *testing.T) {
	quelle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>Login</html>"))
	}))
	defer quelle.Close()
	srv, _, _ := testServer(t)
	call(t, "POST", srv.URL+"/api/v1/downloads", "geheim123456789012", map[string]any{"id": "x", "title": "X", "url": quelle.URL})
	d := warte(t, srv, "x", stateFailed)
	if !strings.Contains(d.Error, "keine Videodatei") {
		t.Fatalf("Fehlertext: %q", d.Error)
	}
}

func TestUngueltigeAdresseUndPfadtricks(t *testing.T) {
	srv, _, _ := testServer(t)
	if code, _ := call(t, "POST", srv.URL+"/api/v1/downloads", "geheim123456789012", map[string]any{"id": "a", "title": "A", "url": "file:///etc/passwd"}); code != 400 {
		t.Fatalf("file:// angenommen: %d", code)
	}
	if got := safeFile("../../etc/passwd"); strings.ContainsAny(got, "/\\") {
		t.Fatalf("safeFile: %q", got)
	}
	if got := folderFor(Download{Folder: "../../x/./y", Kind: "movie"}); strings.Contains(got, "..") {
		t.Fatalf("folderFor: %q", got)
	}
	if within("/a/media", "/a/media/../secret") {
		t.Fatal("within lässt ../ durch")
	}
}
