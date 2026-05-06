# Petra CI and development targets.
#
# From the repository root:
#   make ci

GO ?= go
GOENV ?= GOWORK=off
GOVULNCHECK_VERSION ?= latest
GOVULNCHECK ?= $(GO) run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
STATICCHECK_VERSION ?= latest
STATICCHECK ?= $(GO) run honnef.co/go/tools/cmd/staticcheck@$(STATICCHECK_VERSION)
NPM ?= npm
PKG ?= ./...
EXAMPLE_DIRS ?= examples/mvcweb examples/forms examples/tailwind examples/alpine examples/debugerrors examples/htmx-todo
ASSET_EXAMPLE_DIRS ?= examples/tailwind examples/alpine

.DEFAULT_GOAL := help

.PHONY: help
help:
	@printf '%s\n' 'Petra targets:'
	@printf '  %-18s %s\n' 'make ci' 'run the full CI check set'
	@printf '  %-18s %s\n' 'make test' 'run Petra tests'
	@printf '  %-18s %s\n' 'make test-race' 'run Petra tests with the race detector'
	@printf '  %-18s %s\n' 'make build' 'build Petra packages'
	@printf '  %-18s %s\n' 'make example-test' 'run example app tests'
	@printf '  %-18s %s\n' 'make example-build' 'build example apps'
	@printf '  %-18s %s\n' 'make example-assets' 'install and build example Tailwind/Alpine assets'
	@printf '  %-18s %s\n' 'make vet' 'run go vet for Petra and examples'
	@printf '  %-18s %s\n' 'make staticcheck' 'run staticcheck for Petra and examples'
	@printf '  %-18s %s\n' 'make vulncheck' 'run govulncheck for Petra and examples'
	@printf '  %-18s %s\n' 'make fmt' 'format Petra and example Go files'
	@printf '  %-18s %s\n' 'make fmt-check' 'fail if Go files need formatting'
	@printf '  %-18s %s\n' 'make tidy-check' 'fail if go.mod/go.sum files need tidying'
	@printf '  %-18s %s\n' 'make clean' 'clear Go test caches'

.PHONY: ci
ci: deps tidy-check fmt-check vet staticcheck vulncheck test test-race build example-assets example-test example-build

.PHONY: deps
deps:
	$(GOENV) $(GO) mod download
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) mod download); \
	done

.PHONY: tidy-check
tidy-check:
	$(GOENV) $(GO) mod tidy -diff
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) mod tidy -diff); \
	done

.PHONY: fmt
fmt:
	$(GOENV) $(GO) fmt $(PKG)
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) fmt ./...); \
	done

.PHONY: fmt-check
fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		printf '%s\n%s\n' 'Go files need formatting:' "$$files"; \
		exit 1; \
	fi

.PHONY: vet
vet:
	$(GOENV) $(GO) vet $(PKG)
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) vet ./...); \
	done

.PHONY: staticcheck
staticcheck:
	$(GOENV) $(STATICCHECK) $(PKG)
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(STATICCHECK) ./...); \
	done

.PHONY: vulncheck
vulncheck:
	$(GOENV) $(GOVULNCHECK) $(PKG)
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GOVULNCHECK) ./...); \
	done

.PHONY: test
test:
	$(GOENV) $(GO) test $(PKG)

.PHONY: test-race
test-race:
	$(GOENV) $(GO) test -race $(PKG)

.PHONY: build
build:
	$(GOENV) $(GO) build $(PKG)

.PHONY: example-test
example-test:
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) test ./...); \
	done

.PHONY: example-build
example-build:
	@for dir in $(EXAMPLE_DIRS); do \
		tmp="$$(mktemp -d)"; \
		(cd "$$dir" && $(GOENV) $(GO) build -o "$$tmp/app" .); \
		rm -rf "$$tmp"; \
	done

.PHONY: example-assets
example-assets:
	@for dir in $(ASSET_EXAMPLE_DIRS); do \
		(cd "$$dir" && $(NPM) ci && $(NPM) run assets:build); \
	done

.PHONY: clean
clean:
	$(GOENV) $(GO) clean -testcache
	@for dir in $(EXAMPLE_DIRS); do \
		(cd "$$dir" && $(GOENV) $(GO) clean -testcache); \
	done
