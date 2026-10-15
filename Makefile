.PHONY: all solai solaid clean test

# Build everything
all: solai solaid

# Build the solai session runtime binary → .out/solai
solai:
	CGO_ENABLED=0 go build -o .out/solai ./cmd/solai

# Build the solaid control-plane binary → .out/solaid
solaid:
	CGO_ENABLED=0 go build -o .out/solaid ./cmd/solaid

test:
	go test ./...

clean:
	rm -rf .out
