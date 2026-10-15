.PHONY: all solai solaid generate run clean test

# Build everything
all: solai solaid

# Build the solai session runtime binary → .out/solai
solai:
	CGO_ENABLED=0 go build -o .out/solai ./cmd/solai

# Build the solaid control-plane binary → .out/solaid
solaid:
	CGO_ENABLED=0 go build -o .out/solaid ./cmd/solaid

# Regenerate templ components (web/*.templ → web/*_templ.go)
generate:
	go tool templ generate ./web

# Run the control plane (and web UI) with .env loaded. `-o` lets .env win over
# any variables already exported in your shell.
run: all
	go tool godotenv -o -f .env -- ./.out/solaid --addr 127.0.0.1:8787 --launch "$(CURDIR)/.out/solai"

test:
	go test ./...

clean:
	rm -rf .out
