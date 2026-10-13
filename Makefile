.PHONY: all solai solaid tools token-price clean test test-agent test-tools

# Build everything
all: solai solaid tools

# Build the solai CLI/runtime binary → .out/solai
solai:
	CGO_ENABLED=0 go build -o .out/solai ./solai-agent

# Build the solaid control-plane binary → .out/solaid
solaid:
	CGO_ENABLED=0 go build -o .out/solaid ./solai-agent/cmd/solaid

# Build all tools
tools: token-price

# Build the token-price subagent → tools/token-price/bin/token-price
# (must match the executable path declared in its manifest.json)
token-price:
	go build -o tools/token-price/bin/token-price ./tools/token-price

test: test-agent test-tools

test-agent:
	cd solai-agent && go test ./...

test-tools:
	@for mod in tools/*/go.mod; do \
		dir=$$(dirname "$$mod"); \
		echo "Testing $$dir"; \
		(cd "$$dir" && go test ./...) || exit 1; \
	done

clean:
	rm -rf .out tools/token-price/bin
