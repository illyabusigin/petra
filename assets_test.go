package petra

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestAssetsURLProduction(t *testing.T) {
	assets := newTestAssets(t, false)

	got, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}
	assertAssetURL(t, got, `/static/app-[0-9a-f]{64}\.css`)

	withPrefix, err := assets.URL("/static/app.css")
	if err != nil {
		t.Fatalf("URL() with prefix error = %v", err)
	}
	if withPrefix != got {
		t.Fatalf("URL() with prefix = %q, want %q", withPrefix, got)
	}
}

func TestAssetsURLDevelopment(t *testing.T) {
	assets := newTestAssets(t, true)

	got, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}
	if got != "/static/app.css" {
		t.Fatalf("URL() = %q, want %q", got, "/static/app.css")
	}
}

func TestAssetsURLDevelopmentVersion(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.css"), cssFixture())
	mtime := time.Unix(1700000000, 123)
	if err := os.Chtimes(filepath.Join(dir, "app.css"), mtime, mtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	assets, err := NewAssets(AssetOptions{
		Files: fstest.MapFS{
			"static/app.css": {Data: []byte(cssFixture())},
		},
		Root:   "static",
		Prefix: "/static/",
		Dev:    true,
		DevDir: dir,
	})
	if err != nil {
		t.Fatalf("NewAssets() error = %v", err)
	}

	got, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}
	want := "/static/app.css?v=" + strconv.FormatInt(mtime.UnixNano(), 36)
	if got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestAssetsURLRejectsInvalidNames(t *testing.T) {
	assets := newTestAssets(t, false)

	for _, name := range []string{
		"",
		"../app.css",
		"nested/../../app.css",
		"https://example.com/app.css",
		"//example.com/app.css",
		"/other/app.css",
		"app.css?x=1",
		"app.css#hash",
		"missing.css",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := assets.URL(name); err == nil {
				t.Fatalf("URL(%q) = %q, want error", name, got)
			}
		})
	}
}

func TestAssetsHandlerServesRawAssetWithRevalidationPolicy(t *testing.T) {
	assets := newTestAssets(t, false)

	rec := requestAsset(t, assets, "/static/app.css", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != defaultUnhashedAssetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, defaultUnhashedAssetCacheControl)
	}
	if !strings.Contains(rec.Body.String(), "color: black") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestAssetsHandlerServesHashedAssetWithImmutablePolicy(t *testing.T) {
	assets := newTestAssets(t, false)
	assetURL, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}

	rec := requestAsset(t, assets, assetURL, "br,gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != defaultHashedAssetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, defaultHashedAssetCacheControl)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "br" {
		t.Fatalf("Content-Encoding = %q, want br", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", got)
	}
}

func TestAssetsHandlerRejectsMismatchedHash(t *testing.T) {
	assets := newTestAssets(t, false)

	rec := requestAsset(t, assets, "/static/app-0000000000000000000000000000000000000000000000000000000000000000.css", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAssetsHandlerSupportsConditionalRequests(t *testing.T) {
	assets := newTestAssets(t, false)
	assetURL, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}

	rec := requestAsset(t, assets, assetURL, "br,gzip")
	etag := rec.Header().Get("Etag")
	if etag == "" {
		t.Fatal("first response did not include Etag")
	}

	req := httptest.NewRequest(http.MethodGet, assetURL, nil)
	req.Header.Set("Accept-Encoding", "br,gzip")
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotModified)
	}
}

func TestAssetsHandlerSupportsHead(t *testing.T) {
	assets := newTestAssets(t, false)
	assetURL, err := assets.URL("app.css")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodHead, assetURL, nil)
	rec := httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("HEAD body length = %d, want 0", rec.Body.Len())
	}
	if got := rec.Header().Get("Cache-Control"); got != defaultHashedAssetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, defaultHashedAssetCacheControl)
	}
}

func TestAssetsHandlerDoesNotCompressAlreadyCompressedImages(t *testing.T) {
	assets := newTestAssets(t, false)
	assetURL, err := assets.URL("image.webp")
	if err != nil {
		t.Fatalf("URL() error = %v", err)
	}

	rec := requestAsset(t, assets, assetURL, "br,gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
}

func TestAssetsHandlerDoesNotListDirectories(t *testing.T) {
	assets := newTestAssets(t, false)

	rec := requestAsset(t, assets, "/static/nested/", "")
	if rec.Code != http.StatusMovedPermanently && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want redirect or not found", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("directory response leaked listing: %q", rec.Body.String())
	}
}

func TestAssetsHandlerDevelopmentUsesNoStore(t *testing.T) {
	assets := newTestAssets(t, true)

	rec := requestAsset(t, assets, "/static/app.css", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != defaultDevAssetCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", got, defaultDevAssetCacheControl)
	}
}

func newTestAssets(t *testing.T, dev bool) *Assets {
	t.Helper()

	assets, err := NewAssets(AssetOptions{
		Files: fstest.MapFS{
			"static/app.css":       {Data: []byte(cssFixture())},
			"static/image.webp":    {Data: []byte("RIFF----WEBPVP8 ")},
			"static/nested/secret": {Data: []byte("secret")},
		},
		Root:   "static",
		Prefix: "/static/",
		Dev:    dev,
	})
	if err != nil {
		t.Fatalf("NewAssets() error = %v", err)
	}
	return assets
}

func requestAsset(t *testing.T, assets *Assets, path, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec, req)
	return rec
}

func assertAssetURL(t *testing.T, got, pattern string) {
	t.Helper()

	re := regexp.MustCompile(`^` + pattern + `$`)
	if !re.MatchString(got) {
		t.Fatalf("URL() = %q, want pattern %q", got, pattern)
	}
}

func cssFixture() string {
	return strings.Repeat("body { color: black; }\n", 80)
}

var _ fs.ReadDirFS = fstest.MapFS{}
