.PHONY: all solai solaid tools token-price clean test

# Build everything
all: solai solaid tools

# Build the solai session runtime binary → .out/solai
solai:
	CGO_ENABLED=0 go build -o .out/solai ./cmd/solai

# Build the solaid control-plane binary → .out/solaid
solaid:
	CGO_ENABLED=0 go build -o .out/solaid ./cmd/solaid

# Build all tools
tools: token-price

# Build the token-price subagent → tools/token-price/bin/token-price
# (must match the executable path declared in its manifest.json)
token-price:
	go build -o tools/token-price/bin/token-price ./tools/token-price

test:
	go test ./...

clean:
	rm -rf .out tools/token-price/bin
