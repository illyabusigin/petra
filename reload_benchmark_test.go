package petra

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

var syntheticBenchmarkPageCounts = []int{100, 500, 1000}

func BenchmarkParseDir(b *testing.B) {
	for _, pageCount := range syntheticBenchmarkPageCounts {
		b.Run(fmt.Sprintf("pages=%d", pageCount), func(b *testing.B) {
			fixture := writeSyntheticBenchmarkFixture(b, pageCount)

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tmpl := syntheticBenchmarkTemplate()
				if err := tmpl.ParseDir(fixture.dir); err != nil {
					b.Fatalf("ParseDir() error = %v", err)
				}
			}
		})
	}
}

func BenchmarkReloadPage(b *testing.B) {
	for _, pageCount := range syntheticBenchmarkPageCounts {
		b.Run(fmt.Sprintf("pages=%d", pageCount), func(b *testing.B) {
			fixture := writeSyntheticBenchmarkFixture(b, pageCount)
			tmpl := parseSyntheticBenchmarkFixture(b, fixture)
			event := ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.pagePath), Op: ReloadWrite}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := tmpl.Reload(event)
				if err != nil {
					b.Fatalf("Reload() error = %v", err)
				}
				if result.FullReload || len(result.RebuiltPages) != 1 || result.RebuiltComponents {
					b.Fatalf("page reload result = %+v", result)
				}
			}
		})
	}
}

func BenchmarkReloadSectionLayout(b *testing.B) {
	for _, pageCount := range syntheticBenchmarkPageCounts {
		b.Run(fmt.Sprintf("pages=%d", pageCount), func(b *testing.B) {
			fixture := writeSyntheticBenchmarkFixture(b, pageCount)
			tmpl := parseSyntheticBenchmarkFixture(b, fixture)
			event := ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.sectionLayoutPath), Op: ReloadWrite}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := tmpl.Reload(event)
				if err != nil {
					b.Fatalf("Reload() error = %v", err)
				}
				if result.FullReload || len(result.RebuiltPages) != fixture.pagesPerSection || result.RebuiltComponents {
					b.Fatalf("section layout reload result = %+v", result)
				}
			}
		})
	}
}

func BenchmarkReloadGlobalComponent(b *testing.B) {
	for _, pageCount := range syntheticBenchmarkPageCounts {
		b.Run(fmt.Sprintf("pages=%d", pageCount), func(b *testing.B) {
			fixture := writeSyntheticBenchmarkFixture(b, pageCount)
			tmpl := parseSyntheticBenchmarkFixture(b, fixture)
			event := ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.globalComponentPath), Op: ReloadWrite}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := tmpl.Reload(event)
				if err != nil {
					b.Fatalf("Reload() error = %v", err)
				}
				if result.FullReload || len(result.RebuiltPages) != fixture.pageCount || !result.RebuiltComponents {
					b.Fatalf("global component reload result = %+v", result)
				}
			}
		})
	}
}

func TestSyntheticBenchmarkFixtureReloadScopes(t *testing.T) {
	for _, pageCount := range syntheticBenchmarkPageCounts {
		t.Run(fmt.Sprintf("pages=%d", pageCount), func(t *testing.T) {
			fixture := writeSyntheticBenchmarkFixture(t, pageCount)
			tmpl := parseSyntheticBenchmarkFixture(t, fixture)

			if got := len(tmpl.graph.pageIDs); got != pageCount {
				t.Fatalf("page count = %d, want %d", got, pageCount)
			}

			result, err := tmpl.Reload(ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.pagePath), Op: ReloadWrite})
			if err != nil {
				t.Fatalf("page Reload() error = %v", err)
			}
			if result.FullReload || len(result.RebuiltPages) != 1 || result.RebuiltComponents {
				t.Fatalf("page reload result = %+v", result)
			}

			result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.sectionLayoutPath), Op: ReloadWrite})
			if err != nil {
				t.Fatalf("section layout Reload() error = %v", err)
			}
			if result.FullReload || len(result.RebuiltPages) != fixture.pagesPerSection || result.RebuiltComponents {
				t.Fatalf("section layout reload result = %+v", result)
			}

			result, err = tmpl.Reload(ReloadFileEvent{Path: filepath.Join(fixture.dir, fixture.globalComponentPath), Op: ReloadWrite})
			if err != nil {
				t.Fatalf("global component Reload() error = %v", err)
			}
			if result.FullReload || len(result.RebuiltPages) != pageCount || !result.RebuiltComponents {
				t.Fatalf("global component reload result = %+v", result)
			}
		})
	}
}

type syntheticBenchmarkFixture struct {
	dir                 string
	pageCount           int
	pagesPerSection     int
	pagePath            string
	sectionLayoutPath   string
	globalComponentPath string
}

func writeSyntheticBenchmarkFixture(tb testing.TB, pageCount int) syntheticBenchmarkFixture {
	tb.Helper()

	sections, pagesPerSection := syntheticBenchmarkShape(tb, pageCount)
	dir := tb.TempDir()

	writeSyntheticBenchmarkFile(tb, filepath.Join(dir, "layout.html"), `{{template "shell" .}} root {{block "content" .}}{{end}}`)
	writeSyntheticBenchmarkFile(tb, filepath.Join(dir, "components", "shell.html"), `{{define "shell"}}shell {{logo}} {{global}}{{end}}`)
	writeSyntheticBenchmarkFile(tb, filepath.Join(dir, "components", "global.html"), `{{define "global"}}global v1{{end}}`)
	writeSyntheticBenchmarkFile(tb, filepath.Join(dir, "components", "icons", "logo.html"), `{{define "logo"}}logo{{end}}`)

	for section := range sections {
		sectionName := fmt.Sprintf("section-%03d", section)
		cardName := fmt.Sprintf("Card%03d", section)
		writeSyntheticBenchmarkFile(
			tb,
			filepath.Join(dir, sectionName, "layout.html"),
			fmt.Sprintf(`{{define "content"}}%s {{block "body" .}}{{end}}{{end}}`, sectionName),
		)
		writeSyntheticBenchmarkFile(
			tb,
			filepath.Join(dir, sectionName, "components", "card.html"),
			fmt.Sprintf(`{{define "%s"}}%s card v1{{end}}`, cardName, sectionName),
		)

		for page := range pagesPerSection {
			writeSyntheticBenchmarkFile(
				tb,
				filepath.Join(dir, sectionName, fmt.Sprintf("page-%03d.html", page)),
				fmt.Sprintf(`{{define "body"}}%s page %03d {{%s}}{{end}}`, sectionName, page, cardName),
			)
		}
	}

	return syntheticBenchmarkFixture{
		dir:                 dir,
		pageCount:           pageCount,
		pagesPerSection:     pagesPerSection,
		pagePath:            filepath.ToSlash(filepath.Join("section-000", "page-000.html")),
		sectionLayoutPath:   filepath.ToSlash(filepath.Join("section-000", "layout.html")),
		globalComponentPath: filepath.ToSlash(filepath.Join("components", "global.html")),
	}
}

func syntheticBenchmarkShape(tb testing.TB, pageCount int) (sections, pagesPerSection int) {
	tb.Helper()

	switch pageCount {
	case 100:
		return 10, 10
	case 500:
		return 25, 20
	case 1000:
		return 50, 20
	default:
		tb.Fatalf("unsupported synthetic page count %d", pageCount)
		return 0, 0
	}
}

func parseSyntheticBenchmarkFixture(tb testing.TB, fixture syntheticBenchmarkFixture) *Template {
	tb.Helper()

	tmpl := syntheticBenchmarkTemplate()
	if err := tmpl.ParseDir(fixture.dir); err != nil {
		tb.Fatalf("ParseDir() error = %v", err)
	}
	return tmpl
}

func syntheticBenchmarkTemplate() *Template {
	return NewWithOptions(Options{
		IncludeDir:     "components",
		PageExtensions: []string{".html"},
	})
}

func writeSyntheticBenchmarkFile(tb testing.TB, path, content string) {
	tb.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		tb.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		tb.Fatalf("WriteFile() error = %v", err)
	}
}
