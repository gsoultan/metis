package https

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gsoultan/metis/ui"
)

func get(t *testing.T, h http.Handler, method, target string, gzipOK bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, target, nil)
	if gzipOK {
		req.Header.Set("Accept-Encoding", "gzip")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Building the handler compresses nothing; the first browser to ask for an
// asset has it compressed, and everyone after is sent the same bytes.
func TestAnAssetIsCompressedOnceWhenFirstAskedFor(t *testing.T) {
	dist := testDist()
	h, err := newStaticHandler(dist)
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	asset := h.assets["/assets/app-abc123.js"]
	if asset.gzipped != nil {
		t.Fatal("an asset nobody asked for was compressed at construction")
	}

	first := get(t, h, http.MethodGet, "/assets/app-abc123.js", true)
	if first.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("not compressed: %v", first.Header())
	}
	kept := asset.gzipped
	second := get(t, h, http.MethodGet, "/assets/app-abc123.js", true)
	if !bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) || &kept[0] != &asset.gzipped[0] {
		t.Fatal("the asset was compressed again for the second request")
	}

	zr, err := gzip.NewReader(second.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	plain, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(plain, dist["assets/app-abc123.js"].Data) {
		t.Fatal("the decompressed body is not the asset")
	}
	if h.assets["/assets/app-abc123.css"].gzipped != nil {
		t.Fatal("asking for one asset compressed another")
	}
}

// A client that does not take gzip is sent the asset's own bytes, streamed
// from the dist, with their length; a HEAD is sent the length and no body.
func TestAnUncompressedAssetIsStreamedWithItsLength(t *testing.T) {
	dist := testDist()
	h, _ := newStaticHandler(dist)
	want := dist["assets/app-abc123.css"].Data

	rec := get(t, h, http.MethodGet, "/assets/app-abc123.css", false)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("status %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatal("the body is not the asset")
	}
	if got := rec.Header().Get("Content-Length"); got != "2600" {
		t.Fatalf("Content-Length %q, want 2600", got)
	}

	head := get(t, h, http.MethodHead, "/assets/app-abc123.css", true)
	if head.Body.Len() != 0 || head.Header().Get("Content-Length") == "" {
		t.Fatalf("HEAD sent %d bytes, Content-Length %q", head.Body.Len(), head.Header().Get("Content-Length"))
	}
}

// Browsers opening the app at once after a deploy all ask for the same chunk
// first; it is compressed once and each is sent it whole.
func TestConcurrentFirstRequestsShareOneCompression(t *testing.T) {
	h, _ := newStaticHandler(testDist())
	var wg sync.WaitGroup
	bodies := make([][]byte, 16)
	for i := range bodies {
		wg.Go(func() {
			bodies[i] = get(t, h, http.MethodGet, "/assets/app-abc123.js", true).Body.Bytes()
		})
	}
	wg.Wait()
	for _, b := range bodies {
		if !bytes.Equal(b, bodies[0]) || len(b) == 0 {
			t.Fatal("concurrent first requests were sent different bodies")
		}
	}
}

// BenchmarkNewStaticHandler is what building the handler over the real
// embedded UI costs every replica at boot and every test that builds one.
func BenchmarkNewStaticHandler(b *testing.B) {
	dist := ui.Dist()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := newStaticHandler(dist); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkServeLargestChunkGzipped is a browser fetching the largest chunk,
// once it has been compressed.
func BenchmarkServeLargestChunkGzipped(b *testing.B) {
	h, err := newStaticHandler(ui.Dist())
	if err != nil {
		b.Fatal(err)
	}
	var largest string
	var size int64
	for p, a := range h.assets {
		if a.size > size && a.compressible {
			largest, size = p, a.size
		}
	}
	req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, largest, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	b.ReportAllocs()
	for b.Loop() {
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
}
