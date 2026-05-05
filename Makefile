.PHONY: build test clean install fmt icon syso ci

build:
	go build -o mdm .

test:
	go test -v ./...

ci: fmt
	go test ./...
	go install golang.org/x/vuln/cmd/govulncheck@v1.1.4 && govulncheck ./...
	go install github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 && gocyclo -over 16 .

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
	$(eval VERSION := $(shell grep 'const Version' internal/version/version.go | sed 's/.*"\(.*\)".*/\1/'))
	$(eval VMAJOR  := $(word 1,$(subst ., ,$(VERSION))))
	$(eval VMINOR  := $(word 2,$(subst ., ,$(VERSION))))
	$(eval VPATCH  := $(word 3,$(subst ., ,$(VERSION))))
	go tool goversioninfo \
	    -64 \
	    -ver-major $(VMAJOR) -ver-minor $(VMINOR) -ver-patch $(VPATCH) -ver-build 0 \
	    -product-ver-major $(VMAJOR) -product-ver-minor $(VMINOR) -product-ver-patch $(VPATCH) -product-ver-build 0 \
	    -o resource_windows.syso assets/versioninfo.json
