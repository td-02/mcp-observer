APP := mcpscope
PORT ?= 4444
TRANSPORT ?= stdio

.PHONY: build test run dashboard

build:
	go build ./...

test:
	go test ./...

dashboard:
	cd dashboard && npm run build

run:
	go run . proxy --server "$(SERVER)" --port "$(PORT)" --transport "$(TRANSPORT)"
