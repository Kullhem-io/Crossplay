.PHONY: run build vet gen-types web-dev web-build

# Backend
run:
	go run ./cmd/crossplay

build:
	go build ./...

vet:
	go vet ./...

# Generate TypeScript types from the Go ledger schema (web/src/gen/schema.ts)
gen-types:
	go run github.com/gzuidhof/tygo@v0.2.21 generate

# Front end
web-dev:
	cd web && npm run dev

web-build:
	cd web && npm run build
