package main

import (
	"bytes"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/illyabusigin/petra"
)

func TestEmbeddedAppRendersMountedComponentSet(t *testing.T) {
	a := testApp(t, appOptions{})
	defer closeApp(t, a)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := rec.Body.String()
	for _, want := range []string{
		"Petra component set example",
		`class="ui-stat"`,
		`href="/summary"`,
		"Component set mount",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("response missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/_reload/client.js") {
		t.Fatal("embedded render included hot reload client")
	}
}

func TestExecCanRenderMountedComponentSet(t *testing.T) {
	a := testApp(t, appOptions{})
	defer closeApp(t, a)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/summary", nil)
	a.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	got := rec.Body.String()
	if !strings.Contains(got, `class="ui-stat"`) || !strings.Contains(got, "rendered through Exec") {
		t.Fatalf("summary did not render component-set partial:\n%s", got)
	}
}

func TestDevAppWatchesTemplatesAndComponentSet(t *testing.T) {
	root := copyExampleToTemp(t)
	a := testApp(t, appOptions{
		Dev:     true,
		RootDir: root,
	})
	defer closeApp(t, a)

	wantFolders := []string{
		filepath.Join(root, "templates"),
		filepath.Join(root, "ui", "components"),
	}
	for _, want := range wantFolders {
		if !slices.Contains(a.watchedFolders, want) {
			t.Fatalf("watched folders = %#v, missing %q", a.watchedFolders, want)
		}
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	a.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "/_reload/client.js") {
		t.Fatalf("dev render missing reload client:\n%s", got)
	}
}

func TestDevComponentSetReloadsFromDisk(t *testing.T) {
	root := copyExampleToTemp(t)
	a := testApp(t, appOptions{
		Dev:     true,
		RootDir: root,
	})
	defer closeApp(t, a)

	var out bytes.Buffer
	if err := a.templates.Exec(&out, `{{UI.Badge "Before" "good"}}`, nil); err != nil {
		t.Fatalf("Exec() before edit error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "ui-badge-good") {
		t.Fatalf("before edit badge = %q", got)
	}

	componentFile := filepath.Join(root, "ui", "components", "core.html")
	source, err := os.ReadFile(componentFile)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	edited := strings.Replace(string(source), "ui-badge-good", "ui-badge-reloaded", 1)
	if edited == string(source) {
		t.Fatal("test fixture did not contain ui-badge-good")
	}
	if err := os.WriteFile(componentFile, []byte(edited), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := a.templates.Reload(petra.ReloadFileEvent{
		Path: componentFile,
		Op:   petra.ReloadWrite,
	})
	if err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !result.FullReload {
		t.Fatalf("Reload() FullReload = false, result = %#v", result)
	}

	out.Reset()
	if err := a.templates.Exec(&out, `{{UI.Badge "After" "good"}}`, nil); err != nil {
		t.Fatalf("Exec() after edit error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "ui-badge-reloaded") {
		t.Fatalf("after edit badge = %q", got)
	}
}

func TestDevComponentSetReloadFailureKeepsPreviousTemplates(t *testing.T) {
	root := copyExampleToTemp(t)
	a := testApp(t, appOptions{
		Dev:     true,
		RootDir: root,
	})
	defer closeApp(t, a)

	componentFile := filepath.Join(root, "ui", "components", "core.html")
	if err := os.WriteFile(componentFile, []byte(`{{define "Badge label tone?"}}<span>{{.label}}</span>`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := a.templates.Reload(petra.ReloadFileEvent{Path: componentFile, Op: petra.ReloadWrite}); err == nil {
		t.Fatal("Reload() succeeded for invalid component template")
	}

	var out bytes.Buffer
	if err := a.templates.Exec(&out, `{{UI.Badge "Still works" "good"}}`, nil); err != nil {
		t.Fatalf("Exec() after failed reload error = %v", err)
	}
	if got := out.String(); !strings.Contains(got, "Still works") || !strings.Contains(got, "ui-badge-good") {
		t.Fatalf("previous templates were not preserved:\n%s", got)
	}
}

func testApp(t *testing.T, opts appOptions) app {
	t.Helper()

	opts.Logger = slog.New(slog.NewTextHandler(ioDiscard{}, nil))
	a, err := newApp(opts)
	if err != nil {
		t.Fatalf("newApp() error = %v", err)
	}
	return a
}

func closeApp(t *testing.T, a app) {
	t.Helper()
	if err := a.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func copyExampleToTemp(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	for _, dir := range []string{"templates", "static", filepath.Join("ui", "components")} {
		if err := copyTree(filepath.Join(root, dir), dir); err != nil {
			t.Fatalf("copy %s: %v", dir, err)
		}
	}
	return root
}

func copyTree(dst, src string) error {
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
