.PHONY: build test clean install fmt icon syso ci

build:
	go build -o mdm .

test:
	go test -v ./...

# Mirrors the three jobs in .github/workflows/ci.yml, at the same pinned tool
# versions. GOTOOLCHAIN matches go.mod so the tools are built with the Go that
# can parse this module's sources; tests/devcontainer_test.go fails the build if
# any of these versions drift.
#
# This target does not reformat anything — golangci-lint reports formatting the
# same way CI does. Use `make fmt` (or `golangci-lint fmt`) to fix it.
ci:
	go test ./...
	GOTOOLCHAIN=go1.26.5 go install golang.org/x/vuln/cmd/govulncheck@v1.5.0 && govulncheck ./...
	GOTOOLCHAIN=go1.26.5 go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 && golangci-lint run ./...

clean:
	rm -f mdm resource_windows.syso

install:
	go install .

fmt:
	gofmt -s -w .

# Re-render assets/mdm.ico from the SVG shapes in tools/gen-icon/ (pure Go, no external tools).
# Run this after changing assets/mdm.svg, then commit the updated ICO.
icon:
	go run ./tools/gen-icon/

# Generate resource_windows.syso from the ICO (Windows-only build tag via filename).
# Run this before building the Windows release binary.
# Version is read automatically from internal/version/version.go.
syso: assets/mdm.ico
	$(eval _TAG    := $(or $(GORELEASER_CURRENT_TAG),$(shell grep 'Version =' internal/version/version.go | sed 's/.*"\(.*\)".*/\1/')))
	$(eval _SEMVER := $(shell printf '%s' '$(_TAG)' | sed 's/^v//; s/-.*//' ))
	$(eval VMAJOR  := $(shell printf '%s' '$(_SEMVER)' | cut -d. -f1 | grep -E '^[0-9]+$$' || echo 0))
	$(eval VMINOR  := $(shell printf '%s' '$(_SEMVER)' | cut -d. -f2 | grep -E '^[0-9]+$$' || echo 0))
	$(eval VPATCH  := $(shell printf '%s' '$(_SEMVER)' | cut -d. -f3 | grep -E '^[0-9]+$$' || echo 0))
	go tool goversioninfo \
	    -64 \
	    -ver-major $(VMAJOR) -ver-minor $(VMINOR) -ver-patch $(VPATCH) -ver-build 0 \
	    -product-ver-major $(VMAJOR) -product-ver-minor $(VMINOR) -product-ver-patch $(VPATCH) -product-ver-build 0 \
	    -o resource_windows.syso assets/versioninfo.json
