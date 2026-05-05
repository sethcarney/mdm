.PHONY: build test clean install icon syso

build:
	go build -o mdm .

test:
	go test -v ./...

clean:
	rm -f mdm resource_windows.syso

install:
	go install .

# Render the SVG to a multi-resolution ICO (requires librsvg2-bin + imagemagick).
icon: assets/mdm.ico

assets/mdm.ico: assets/mdm.svg
	@for size in 256 128 64 48 32 16; do \
		rsvg-convert -w $$size -h $$size assets/mdm.svg > assets/mdm_$${size}.png; \
	done
	convert assets/mdm_256.png assets/mdm_128.png assets/mdm_64.png \
	        assets/mdm_48.png  assets/mdm_32.png  assets/mdm_16.png  \
	        assets/mdm.ico
	@rm -f assets/mdm_256.png assets/mdm_128.png assets/mdm_64.png \
	       assets/mdm_48.png  assets/mdm_32.png  assets/mdm_16.png

# Generate resource_windows.syso from the ICO (Windows-only build tag via filename).
# Run this before building the Windows release binary.
syso: assets/mdm.ico
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 \
	    -o resource_windows.syso assets/versioninfo.json
