.PHONY: build run test clean

PKGS := ./cmd/... ./internal/...

build:
	go build -o bin/wit-stitch ./cmd/wit-stitch

# usage: make run URL=https://example.com/zoomify/Map/TileGroup0/0-0-0.jpg
run: build
	./bin/wit-stitch $(URL)

test:
	go vet $(PKGS)
	go test -race $(PKGS)

clean:
	rm -rf bin
