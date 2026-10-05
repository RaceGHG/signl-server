package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// engine lädt Dateien herunter: normale Adressen per HTTP mit Fortsetzen,
// HLS-Listen (.m3u8) über ffmpeg, falls installiert.
type engine struct {
	store    *store
	mediaDir string
	sem      chan struct{}
	client   *http.Client

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	wg      sync.WaitGroup
}

func newEngine(s *store, mediaDir string, parallel int) *engine {
	if parallel < 1 {
		parallel = 2
	}
	return &engine{
		store:    s,
		mediaDir: mediaDir,
		sem:      make(chan struct{}, parallel),
		cancels:  map[string]context.CancelFunc{},
		client: &http.Client{
			// Kein Gesamt-Timeout: Filme dauern. Nur das Warten auf Antwort ist begrenzt.
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				ResponseHeaderTimeout: 30 * time.Second,
				TLSHandshakeTimeout:   15 * time.Second,
				IdleConnTimeout:       30 * time.Second,
			},
		},
	}
}

var ffmpegPath, _ = exec.LookPath("ffmpeg")

func hasFFmpeg() bool { return ffmpegPath != "" }

// resume setzt nach einem Neustart unterbrochene Downloads fort.
func (e *engine) resume() {
	for _, d := range e.store.snapshot() {
		if d.State == stateDownloading || d.State == stateQueued {
			e.start(d.ID)
		}
	}
}

func (e *engine) stopAll() {
	e.mu.Lock()
	for _, c := range e.cancels {
		c()
	}
	e.mu.Unlock()
	e.wg.Wait()
}

// start reiht einen Download ein.
func (e *engine) start(id string) {
	e.mu.Lock()
	if _, laeuft := e.cancels[id]; laeuft {
		e.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.cancels[id] = cancel
	e.mu.Unlock()

	e.store.update(id, true, func(d *Download) {
		d.State = stateQueued
		d.Error = ""
	})
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			e.mu.Lock()
			delete(e.cancels, id)
			e.mu.Unlock()
			cancel()
		}()
		select {
		case e.sem <- struct{}{}:
			defer func() { <-e.sem }()
		case <-ctx.Done():
			return
		}
		e.run(ctx, id)
	}()
}

// cancel bricht einen laufenden Download ab (Teildatei bleibt nicht liegen).
func (e *engine) cancel(id string) {
	e.mu.Lock()
	c := e.cancels[id]
	e.mu.Unlock()
	if c != nil {
		c()
	}
	e.store.update(id, true, func(d *Download) {
		if d.State != stateCompleted {
			d.State = stateCancelled
		}
	})
	e.removePartial(id)
}

func (e *engine) partPath(id string) string {
	return filepath.Join(e.mediaDir, ".partial", safeFile(id)+".part")
}

func (e *engine) removePartial(id string) {
	_ = os.Remove(e.partPath(id))
}

func (e *engine) run(ctx context.Context, id string) {
	d, ok := e.store.get(id)
	if !ok {
		return
	}
	e.store.update(id, true, func(d *Download) { d.State = stateDownloading })

	var err error
	for versuch := 0; versuch < 4; versuch++ {
		if versuch > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(versuch*versuch) * 3 * time.Second):
			}
		}
		if isHLS(d.SourceURL) {
			err = e.runHLS(ctx, d)
		} else {
			err = e.runHTTP(ctx, d)
		}
		if err == nil || ctx.Err() != nil || errors.Is(err, errFatal) {
			break
		}
	}
	if ctx.Err() != nil {
		return // abgebrochen: Zustand setzt cancel()
	}
	if err != nil {
		msg := err.Error()
		e.store.update(id, true, func(d *Download) {
			d.State = stateFailed
			d.Error = msg
		})
	}
}

// errFatal: Wiederholen hilft nicht (falsche Adresse, 4xx, kein ffmpeg).
var errFatal = errors.New("fatal")

func fatal(format string, a ...any) error {
	return fmt.Errorf("%s: %w", fmt.Sprintf(format, a...), errFatal)
}

func isHLS(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".m3u8")
}

var knownExt = map[string]bool{
	".mp4": true, ".mkv": true, ".avi": true, ".ts": true, ".m4v": true,
	".mov": true, ".webm": true, ".mpg": true, ".mpeg": true, ".wmv": true, ".flv": true,
}

func extensionFor(rawURL, contentType string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if ext := strings.ToLower(path.Ext(u.Path)); knownExt[ext] {
			return ext
		}
	}
	mt, _, _ := mime.ParseMediaType(contentType)
	switch mt {
	case "video/x-matroska":
		return ".mkv"
	case "video/mp2t":
		return ".ts"
	case "video/webm":
		return ".webm"
	case "video/quicktime":
		return ".mov"
	case "video/x-msvideo":
		return ".avi"
	}
	return ".mp4"
}

func (e *engine) runHTTP(ctx context.Context, d Download) error {
	part := e.partPath(d.ID)
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return fatal("Ordner nicht beschreibbar: %v", err)
	}
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.SourceURL, nil)
	if err != nil {
		return fatal("Ungültige Adresse")
	}
	req.Header.Set("User-Agent", "SIGNL-Server/"+Version)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		have = 0 // Server ignoriert Range → von vorn
	case resp.StatusCode == http.StatusPartialContent:
	case resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && have > 0:
		_ = os.Remove(part)
		return errors.New("Teildatei verworfen")
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return fatal("Der Anbieter antwortet mit %d", resp.StatusCode)
	default:
		return fmt.Errorf("Antwort %d", resp.StatusCode)
	}

	total := resp.ContentLength
	if total > 0 {
		total += have
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/") || strings.Contains(ct, "mpegurl") {
		if strings.Contains(ct, "mpegurl") {
			return e.runHLS(ctx, d)
		}
		return fatal("Die Adresse liefert keine Videodatei (%s)", ct)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if have == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return fatal("Datei nicht beschreibbar: %v", err)
	}

	e.store.update(d.ID, false, func(x *Download) { x.Total = total; x.Bytes = have })

	buf := make([]byte, 256<<10)
	written := have
	last := time.Now()
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return fatal("Schreiben fehlgeschlagen: %v", werr)
			}
			written += int64(n)
			if time.Since(last) > 500*time.Millisecond {
				last = time.Now()
				w := written
				e.store.update(d.ID, false, func(x *Download) {
					x.Bytes = w
					if total > 0 {
						x.Progress = float64(w) / float64(total)
					}
				})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	if total > 0 && written < total {
		return io.ErrUnexpectedEOF
	}
	final, err := e.finalize(d, part, extensionFor(d.SourceURL, resp.Header.Get("Content-Type")))
	if err != nil {
		return fatal("Ablegen fehlgeschlagen: %v", err)
	}
	e.complete(d.ID, final, written)
	return nil
}

var outTime = regexp.MustCompile(`^out_time_(?:us|ms)=(\d+)`)

func (e *engine) runHLS(ctx context.Context, d Download) error {
	if !hasFFmpeg() {
		return fatal("Für Streams (HLS) wird ffmpeg auf dem Server gebraucht. Das Docker-Image enthält es.")
	}
	part := e.partPath(d.ID) + ".mp4"
	if err := os.MkdirAll(filepath.Dir(part), 0o755); err != nil {
		return fatal("Ordner nicht beschreibbar: %v", err)
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-nostdin", "-y", "-loglevel", "error",
		"-progress", "pipe:1", "-i", d.SourceURL, "-c", "copy",
		"-bsf:a", "aac_adtstoasc", "-f", "mp4", part)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf strings.Builder
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return fatal("ffmpeg startet nicht: %v", err)
	}
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if m := outTime.FindStringSubmatch(sc.Text()); m != nil && d.Duration > 0 {
			us, _ := strconv.ParseFloat(m[1], 64)
			p := us / 1e6 / d.Duration
			if p > 0.99 {
				p = 0.99
			}
			e.store.update(d.ID, false, func(x *Download) { x.Progress = p })
		}
	}
	if err := cmd.Wait(); err != nil {
		_ = os.Remove(part)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fatal("ffmpeg: %s", strings.TrimSpace(errBuf.String()))
	}
	st, _ := os.Stat(part)
	var size int64
	if st != nil {
		size = st.Size()
	}
	final, err := e.finalize(d, part, ".mp4")
	if err != nil {
		return fatal("Ablegen fehlgeschlagen: %v", err)
	}
	e.complete(d.ID, final, size)
	return nil
}

func (e *engine) complete(id, rel string, size int64) {
	now := time.Now()
	e.store.update(id, true, func(x *Download) {
		x.State = stateCompleted
		x.Progress = 1
		x.File = rel
		x.Bytes = size
		x.Total = size
		x.Error = ""
		x.FinishedAt = &now
	})
}

// finalize verschiebt die Teildatei an ihren endgültigen Platz:
// media/<Ordner>/<Titel>.<Endung>, mit „(2)" bei gleichem Namen.
func (e *engine) finalize(d Download, part, ext string) (string, error) {
	dir := filepath.Join(e.mediaDir, folderFor(d))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := safeFile(d.Title)
	if base == "" {
		base = safeFile(d.ID)
	}
	if d.Subtitle != "" && d.Kind == "episode" {
		base += " - " + safeFile(d.Subtitle)
	}
	name := base + ext
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(dir, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s (%d)%s", base, i, ext)
	}
	dst := filepath.Join(dir, name)
	if err := os.Rename(part, dst); err != nil {
		return "", err
	}
	rel, _ := filepath.Rel(e.mediaDir, dst)
	return filepath.ToSlash(rel), nil
}

func folderFor(d Download) string {
	var parts []string
	if d.Folder != "" {
		for _, p := range strings.Split(d.Folder, "/") {
			if s := safeFile(p); s != "" {
				parts = append(parts, s)
			}
		}
	}
	if len(parts) > 0 {
		return filepath.Join(parts...)
	}
	switch d.Kind {
	case "movie":
		return "Filme"
	case "series", "episode":
		return "Serien"
	}
	return "Sonstiges"
}

var unsafeChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// safeFile macht aus beliebigem Text einen Datei- bzw. Ordnernamen, der auf
// Windows, macOS und Linux gleich gültig ist.
func safeFile(s string) string {
	s = unsafeChars.ReplaceAllString(strings.TrimSpace(s), "_")
	s = strings.Trim(s, ". ")
	if s == ".." || s == "." {
		return ""
	}
	r := []rune(s)
	if len(r) > 120 {
		s = strings.TrimSpace(string(r[:120]))
	}
	return s
}
