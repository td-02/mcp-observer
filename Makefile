APP := mcpscope
PORT ?= 4444
TRANSPORT ?= stdio

.PHONY: build test run dashboard dashboard-build demo demo-clean demo-preview

dashboard-build:
	cd dashboard && npm ci && npm run build

dashboard: dashboard-build

build: dashboard-build
	go build ./...

test: dashboard-build
	go test ./...

run:
ifndef SERVER
	$(error Run with: make run SERVER=./path/to/your-mcp-server)
endif
	go run . proxy --server "$(SERVER)" --port "$(PORT)" --transport "$(TRANSPORT)"

demo: ## Generate demo GIF, MP4, and teaser GIF
	@sh demo/record.sh

demo-clean: ## Remove generated demo assets
	@rm -f demo/mcpscope-demo.gif demo/mcpscope-demo.mp4 demo/mcpscope-teaser.gif
	@echo "Demo assets cleaned."

demo-preview: ## Open the generated GIF in the default viewer
	@open demo/mcpscope-demo.gif 2>/dev/null || xdg-open demo/mcpscope-demo.gif
