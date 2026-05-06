# Petra benchmarks

Petra has synthetic benchmarks for full parsing and selective reload behavior.
The fixtures are generated at benchmark time and cover exact 100, 500, and
1000 page trees.

Each tree has:

- a root layout;
- root components, including a nested component;
- section layouts;
- section-local components;
- pages that combine root and section dependencies.

Run the current benchmark set from the repository root:

```sh
GOWORK=off go test -run '^$' -bench 'Benchmark(ParseDir|Reload(Page|SectionLayout|GlobalComponent))$' -benchmem -benchtime=1s -count=3 ./
```

## Local baseline

Recorded on 2026-04-29 with Go `1.26.2` on `darwin/arm64`, Apple M2.
Values are median `ns/op` from `-count=3`, rounded for readability.

| Benchmark | 100 pages | 500 pages | 1000 pages |
| --- | ---: | ---: | ---: |
| `BenchmarkParseDir` | 22.10 ms | 116.63 ms | 236.28 ms |
| `BenchmarkReloadPage` | 211 us | 224 us | 236 us |
| `BenchmarkReloadSectionLayout` | 2.20 ms | 4.13 ms | 4.24 ms |
| `BenchmarkReloadGlobalComponent` | 21.64 ms | 104.56 ms | 216.43 ms |

The fixture shape explains the numbers:

- Page reload reparses one page and stays nearly flat as the site grows.
- Section layout reload reparses one section. The 100-page fixture has 10 pages
  in the edited section; the 500 and 1000-page fixtures have 20.
- Global component reload reparses every page and rebuilds the component set, so
  it should track full parse cost.

When parser or reload code changes, rerun the command above and record the new
numbers next to the change.
