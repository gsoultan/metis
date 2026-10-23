package https

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/rs/zerolog/log"
)

// staticHandler serves the embedded UI: the built assets and, for any path the
// single-page app owns, index.html.
//
// It exists because the previous handler was a bare http.FileServerFS, which
// sent every asset uncompressed and with no Cache-Control. A first paint was
// ~1.25 MB on the wire when the same bytes gzip to ~330 kB, and every reload
// refetched the whole app because the browser was never told it could keep
// anything. The distroless image has no reverse proxy to add either, so this is
// where it has to happen.
//
// The dist is embedded and immutable for the life of the process, so each
// file's type and ETag are worked out once, at construction, and each one is
// gzipped once — the first time a browser asks for it. Serving is then a map
// lookup and a write: no per-request compression, no per-request stat.
//
// It used to read and gzip every file up front and keep both copies: the
// heaviest thing the server did at boot (0.6 s of CPU at the best compression
// level, for every replica and every test that built a handler) and about a
// quarter of the heap of an idle server, mostly spent on fonts and chunks a
// session never opens. The uncompressed bytes already live in the binary, so
// they are streamed from there instead of copied, and only the gzipped bytes a
// browser has asked for are held.
type staticHandler struct {
	files  fs.FS
	assets map[string]*staticAsset
	index  *staticAsset
}

type staticAsset struct {
	path        string
	contentType string
	etag        string
	size        int64
	// immutable is true for content-hashed assets under /assets, which may be
	// cached forever; index.html and the rest must be revalidated so a deploy
	// is seen.
	immutable bool
	// compressible is whether gzipping the type can pay off at all; images and
	// fonts are compressed already.
	compressible bool

	gzipOnce sync.Once
	gzipped  []byte // nil when compressing did not pay off
}

func newStaticHandler(distFS fs.FS) (*staticHandler, error) {
	h := &staticHandler{files: distFS, assets: map[string]*staticAsset{}}
	// One buffer for the walk, rather than io.Copy's fresh 32 kB per file.
	buf := make([]byte, 32*1024)
	err := fs.WalkDir(distFS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		a, err := describeAsset(distFS, p, buf)
		if err != nil {
			return err
		}
		h.assets["/"+p] = a
		if p == "index.html" {
			h.index = a
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return h, nil
}

// contentTypeOverrides are the extensions the platform's own MIME table gets
// wrong or does not know. A web app manifest served as text/plain is ignored by
// the browser, and the app is then simply not installable — with nothing in the
// console to say why.
var contentTypeOverrides = map[string]string{
	".webmanifest": "application/manifest+json",
	".json":        "application/json",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".svg":         "image/svg+xml",
	".wasm":        "application/wasm",
}

// sniffLength is how much of a file http.DetectContentType reads.
const sniffLength = 512

// describeAsset works out what serving one file needs to know, reading it
// through once, by way of buf, without keeping it.
func describeAsset(distFS fs.FS, p string, buf []byte) (*staticAsset, error) {
	f, err := distFS.Open(p)
	if err != nil {
		return nil, err
	}
	defer closeAsset(f, p)

	digest := sha256.New()
	head := &prefixBuffer{limit: sniffLength}
	size, err := io.CopyBuffer(io.MultiWriter(digest, head), f, buf)
	if err != nil {
		return nil, err
	}

	ext := path.Ext(p)
	ct, ok := contentTypeOverrides[ext]
	if !ok {
		ct = mime.TypeByExtension(ext)
	}
	if ct == "" {
		ct = http.DetectContentType(head.bytes)
	}
	sum := digest.Sum(nil)
	return &staticAsset{
		path:        p,
		contentType: ct,
		etag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
		size:        size,
		// Vite writes content-hashed names under assets/, so the bytes behind a
		// name never change: cache them forever. Everything else is revalidated.
		immutable:    strings.HasPrefix(p, "assets/"),
		compressible: compressibleType(ct),
	}, nil
}

// prefixBuffer keeps the first limit bytes written to it and discards the rest.
type prefixBuffer struct {
	limit int
	bytes []byte
}

func (b *prefixBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.bytes); room > 0 {
		b.bytes = append(b.bytes, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// gzippedFrom is the asset gzipped, compressed on first use and kept; nil when
// compressing does not pay off.
func (a *staticAsset) gzippedFrom(distFS fs.FS) []byte {
	if !a.compressible {
		return nil
	}
	a.gzipOnce.Do(func() {
		raw, err := fs.ReadFile(distFS, a.path)
		if err != nil {
			// Served uncompressed instead, which is slower but correct.
			log.Warn().Err(err).Str("path", a.path).Msg("Could not read an embedded asset to compress it")
			return
		}
		if gz := gzipBytes(raw); len(gz) > 0 && len(gz) < len(raw)-64 {
			a.gzipped = gz
		}
	})
	return a.gzipped
}

func gzipBytes(raw []byte) []byte {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := zw.Write(raw); err != nil {
		return nil
	}
	if err := zw.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

func compressibleType(ct string) bool {
	ct = strings.ToLower(ct)
	switch {
	case strings.HasPrefix(ct, "text/"),
		strings.Contains(ct, "javascript"),
		strings.Contains(ct, "json"),
		strings.Contains(ct, "svg"),
		strings.Contains(ct, "xml"),
		strings.Contains(ct, "wasm"):
		return true
	}
	return false
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	asset := h.resolve(r.URL.Path)
	if asset == nil {
		http.NotFound(w, r)
		return
	}

	header := w.Header()
	header.Set("Content-Type", asset.contentType)
	header.Set("ETag", asset.etag)
	header.Set("X-Content-Type-Options", "nosniff")
	if asset.immutable {
		header.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// index.html and friends carry the references to the hashed assets, so
		// a stale one pins the browser to an old deploy. Always revalidate.
		header.Set("Cache-Control", "no-cache")
	}

	if match := r.Header.Get("If-None-Match"); match != "" && etagMatches(match, asset.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if acceptsGzip(r) {
		if gz := asset.gzippedFrom(h.files); gz != nil {
			header.Set("Content-Encoding", "gzip")
			header.Add("Vary", "Accept-Encoding")
			h.write(w, r, gz)
			return
		}
	}
	h.stream(w, r, asset)
}

// write sends bytes already in memory.
//
// Written directly rather than through http.ServeContent: a Range request
// over the gzipped bytes would otherwise be answered with byte offsets into
// the compressed stream, which no client can reassemble. The assets are
// small and fully in memory, so range support buys nothing here.
func (h *staticHandler) write(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := w.Write(body); err != nil {
		logIncompleteWrite(r, err)
	}
}

// stream sends an asset uncompressed, straight from the embedded dist.
func (h *staticHandler) stream(w http.ResponseWriter, r *http.Request, asset *staticAsset) {
	w.Header().Set("Content-Length", strconv.FormatInt(asset.size, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	f, err := h.files.Open(asset.path)
	if err != nil {
		// Embedded and listed at construction, so this cannot happen short of
		// a corrupt binary; the headers are not sent yet, so say so properly.
		w.Header().Del("Content-Length")
		http.Error(w, "Asset unavailable", http.StatusInternalServerError)
		log.Error().Err(err).Str("path", asset.path).Msg("An embedded asset listed at startup could not be opened")
		return
	}
	defer closeAsset(f, asset.path)
	if _, err := io.Copy(w, f); err != nil {
		logIncompleteWrite(r, err)
	}
}

// closeAsset closes a file of the embedded dist. Nothing is written through
// it, so a failure loses nothing, but it would mean the dist is not what it
// was at construction.
func closeAsset(f fs.File, p string) {
	if err := f.Close(); err != nil {
		log.Debug().Err(err).Str("path", p).Msg("Could not close an embedded asset")
	}
}

// logIncompleteWrite records a write that did not complete. That is the
// client having gone away mid-response, which is normal and not actionable —
// the status line is already sent, so there is nothing left to tell them.
// Logged at debug rather than discarded so a genuine pattern of truncated
// assets is still findable.
func logIncompleteWrite(r *http.Request, err error) {
	log.Debug().Err(err).Str("path", r.URL.Path).Msg("Static asset write did not complete")
}

// resolve maps a request path to an asset, falling back to index.html for the
// paths the single-page router owns. A missing file under /assets is a genuine
// 404 — it is a hashed name that should exist — rather than the app shell, so a
// broken asset reference does not silently return HTML with a JS content type.
func (h *staticHandler) resolve(urlPath string) *staticAsset {
	if urlPath == "/" || urlPath == "" {
		return h.index
	}
	if a, ok := h.assets[urlPath]; ok {
		return a
	}
	if strings.HasPrefix(urlPath, "/assets/") {
		return nil
	}
	return h.index
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]), "gzip") {
			return true
		}
	}
	return false
}

func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag || candidate == "*" {
			return true
		}
	}
	return false
}

// securityHeaders wraps a handler with the response headers an enterprise buyer
// expects to find, and the CSP that makes the bearer token in localStorage
// harder to exfiltrate if a script ever slips in.
//
// The policy is written for exactly what the UI loads: its own bundle, Mantine's
// inline styles, Google Fonts, same-origin API and SSE, and a service worker.
// Nothing cross-origin is fetched beyond fonts, so the rest is 'self'.
func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; " +
		"script-src 'self'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com data:; " +
		"img-src 'self' data: blob:; " +
		"connect-src 'self'; " +
		"worker-src 'self' blob:; " +
		"manifest-src 'self'; " +
		"object-src 'none'; " +
		"base-uri 'self'; " +
		"form-action 'self'; " +
		"frame-ancestors 'none'"

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// HSTS is only meaningful over TLS, which this server does not terminate
		// itself; the ingress adds it. Set it anyway so a direct HTTPS terminator
		// in front inherits a sane default. Harmless over plain HTTP: browsers
		// ignore it there.
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}
