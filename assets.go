package petra

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/benbjohnson/hashfs"
	"github.com/vearutop/statigz"
	"github.com/vearutop/statigz/brotli"
)

const (
	defaultHashedAssetCacheControl   = "public, max-age=31536000, immutable"
	defaultUnhashedAssetCacheControl = "no-cache"
	defaultDevAssetCacheControl      = "no-store"
)

// AssetOptions configures an asset URL helper and production static asset
// handler.
type AssetOptions struct {
	// Files is the embedded or virtual filesystem containing the asset tree.
	Files fs.ReadDirFS

	// Root is the asset root inside Files. When empty, Files is used as-is.
	Root string

	// Prefix is the request path prefix used for generated URLs. When empty,
	// "/" is used.
	Prefix string

	// Dev switches URL generation and cache headers to development behavior.
	Dev bool

	// DevDir is the local filesystem directory for development assets. When
	// set, generated dev URLs include an mtime query string.
	DevDir string

	// CacheControlHashed overrides the cache policy for verified hashed asset
	// URLs.
	CacheControlHashed string

	// CacheControlUnhashed overrides the cache policy for raw asset URLs.
	CacheControlUnhashed string

	// CacheControlDev overrides the cache policy when Dev is true.
	CacheControlDev string
}

// Assets generates cache-safe asset URLs and serves matching static files.
type Assets struct {
	files fs.ReadDirFS
	hash  *hashfs.FS

	prefix string
	dev    bool
	devDir string

	cacheControlHashed   string
	cacheControlUnhashed string
	cacheControlDev      string

	server http.Handler
}

// NewAssets creates an asset helper and handler. In production, URL returns
// content-hashed paths and Handler serves verified hashed requests with
// immutable cache headers while keeping Brotli/gzip support from statigz.
func NewAssets(opts AssetOptions) (*Assets, error) {
	if opts.Files == nil {
		return nil, errors.New("petra: asset files are required")
	}

	root, err := cleanAssetRoot(opts.Root)
	if err != nil {
		return nil, err
	}

	assetFS, err := assetSubFS(opts.Files, root)
	if err != nil {
		return nil, err
	}

	cacheControlHashed := opts.CacheControlHashed
	if cacheControlHashed == "" {
		cacheControlHashed = defaultHashedAssetCacheControl
	}
	cacheControlUnhashed := opts.CacheControlUnhashed
	if cacheControlUnhashed == "" {
		cacheControlUnhashed = defaultUnhashedAssetCacheControl
	}
	cacheControlDev := opts.CacheControlDev
	if cacheControlDev == "" {
		cacheControlDev = defaultDevAssetCacheControl
	}

	a := &Assets{
		files:                assetFS,
		hash:                 hashfs.NewFS(assetFS),
		prefix:               normalizeAssetPrefix(opts.Prefix),
		dev:                  opts.Dev,
		devDir:               opts.DevDir,
		cacheControlHashed:   cacheControlHashed,
		cacheControlUnhashed: cacheControlUnhashed,
		cacheControlDev:      cacheControlDev,
		server:               statigz.FileServer(assetFS, brotli.AddEncoding, statigz.EncodeOnInit),
	}

	return a, nil
}

// URL returns the public URL for an asset name. Production URLs include a
// content hash. Development URLs remain readable and can include an mtime query
// string when DevDir is configured.
func (a *Assets) URL(name string) (string, error) {
	if a == nil {
		return "", errors.New("petra: nil assets")
	}

	asset, err := a.cleanName(name)
	if err != nil {
		return "", err
	}

	if a.dev {
		if a.devDir != "" {
			version, err := a.devVersion(asset)
			if err != nil {
				return "", err
			}
			return a.urlPath(asset) + "?v=" + version, nil
		}
		if err := a.assetExists(asset); err != nil {
			return "", err
		}
		return a.urlPath(asset), nil
	}

	hashed, err := a.hashName(asset)
	if err != nil {
		return "", err
	}
	return a.urlPath(hashed), nil
}

// Handler returns an HTTP handler for the configured assets.
func (a *Assets) Handler() http.Handler {
	if a == nil {
		return http.NotFoundHandler()
	}
	return http.HandlerFunc(a.serveHTTP)
}

func (a *Assets) serveHTTP(w http.ResponseWriter, r *http.Request) {
	asset, ok := a.requestName(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	cacheControl := a.cacheControlUnhashed
	target := asset

	if a.dev {
		cacheControl = a.cacheControlDev
	} else if base, hash := hashfs.ParseName(asset); hash != "" {
		expected, err := a.hashName(base)
		if err != nil || expected != asset {
			http.NotFound(w, r)
			return
		}
		cacheControl = a.cacheControlHashed
		target = base
	}

	if cacheControl != "" {
		w.Header().Set("Cache-Control", cacheControl)
	}

	next := r.Clone(r.Context())
	next.URL = new(url.URL)
	*next.URL = *r.URL
	next.URL.Path = "/" + target
	next.URL.RawPath = ""

	a.server.ServeHTTP(w, next)
}

func (a *Assets) hashName(name string) (string, error) {
	if err := a.assetExists(name); err != nil {
		return "", err
	}

	hashed := a.hash.HashName(name)
	if hashed == name {
		return "", fmt.Errorf("petra: hash asset %q", name)
	}
	return hashed, nil
}

func (a *Assets) assetExists(name string) error {
	info, err := fs.Stat(a.files, name)
	if err != nil {
		return fmt.Errorf("petra: asset %q: %w", name, err)
	}
	if info.IsDir() {
		return fmt.Errorf("petra: asset %q is a directory", name)
	}
	return nil
}

func (a *Assets) devVersion(name string) (string, error) {
	info, err := os.Stat(filepath.Join(a.devDir, filepath.FromSlash(name)))
	if err != nil {
		return "", fmt.Errorf("petra: dev asset %q: %w", name, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("petra: dev asset %q is a directory", name)
	}
	return strconv.FormatInt(info.ModTime().UnixNano(), 36), nil
}

func (a *Assets) cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("petra: asset name is empty")
	}
	if strings.ContainsAny(name, "?#") {
		return "", fmt.Errorf("petra: asset %q must not include query or fragment", name)
	}
	if parsed, err := url.Parse(name); err == nil && (parsed.IsAbs() || parsed.Host != "") {
		return "", fmt.Errorf("petra: asset %q must be a local path", name)
	}

	if strings.HasPrefix(name, "/") {
		trimmed, ok := trimAssetPrefix(name, a.prefix)
		if !ok {
			return "", fmt.Errorf("petra: asset %q is outside prefix %q", name, a.prefix)
		}
		name = trimmed
	}

	return cleanAssetRelativePath(name)
}

func (a *Assets) requestName(requestPath string) (string, bool) {
	trimmed, ok := trimAssetPrefix(requestPath, a.prefix)
	if !ok {
		return "", false
	}

	cleaned, err := cleanAssetRelativePath(trimmed)
	if err != nil {
		return "", false
	}
	return cleaned, true
}

func (a *Assets) urlPath(name string) string {
	if a.prefix == "/" {
		return "/" + name
	}
	return a.prefix + name
}

func assetSubFS(files fs.ReadDirFS, root string) (fs.ReadDirFS, error) {
	if root == "." {
		return files, nil
	}

	sub, err := fs.Sub(files, root)
	if err != nil {
		return nil, fmt.Errorf("petra: asset root %q: %w", root, err)
	}
	readDir, ok := sub.(fs.ReadDirFS)
	if !ok {
		return nil, fmt.Errorf("petra: asset root %q does not implement fs.ReadDirFS", root)
	}
	return readDir, nil
}

func cleanAssetRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" || root == "." {
		return ".", nil
	}
	root = strings.Trim(root, "/")
	cleaned, err := cleanAssetRelativePath(root)
	if err != nil {
		return "", fmt.Errorf("petra: asset root %q is invalid: %w", root, err)
	}
	return cleaned, nil
}

func cleanAssetRelativePath(value string) (string, error) {
	value = strings.TrimPrefix(value, "/")
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == "" {
		return "", errors.New("empty path")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("path escapes asset root: %q", value)
	}
	return cleaned, nil
}

func normalizeAssetPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = path.Clean(prefix)
	if prefix == "/" {
		return prefix
	}
	return prefix + "/"
}

func trimAssetPrefix(value, prefix string) (string, bool) {
	if !strings.HasPrefix(value, "/") {
		return value, true
	}
	if prefix == "/" {
		return strings.TrimPrefix(value, "/"), true
	}
	if strings.HasPrefix(value, prefix) {
		return strings.TrimPrefix(value, prefix), true
	}
	return "", false
}
