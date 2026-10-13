module github.com/melodyogonna/solai/raydium

go 1.25.7

require github.com/joho/godotenv v1.5.1

require (
	github.com/melodyogonna/solai/ipc v0.0.0
	github.com/melodyogonna/solai/llm v0.0.0
)

replace github.com/melodyogonna/solai/llm => ../../llm

replace github.com/melodyogonna/solai/ipc => ../../ipc
