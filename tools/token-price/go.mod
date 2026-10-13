module github.com/melodyogonna/solai/tools/token-price

go 1.25.7

require (
	github.com/joho/godotenv v1.5.1
	github.com/melodyogonna/solai/ratelimit v0.0.0-20260306215652-f12013591d3c
)

require (
	github.com/melodyogonna/solai/ipc v0.0.0
	github.com/melodyogonna/solai/llm v0.0.0
)

replace github.com/melodyogonna/solai/llm => ../../llm

replace github.com/melodyogonna/solai/ipc => ../../ipc
