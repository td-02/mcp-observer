package main

import (
	"fmt"
	"net/http"

	"github.com/td-02/mcp-observer/sdk/go/mcpscope"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"jsonrpc":"2.0","result":{"ok":true},"id":1}`)
	})

	handler := mcpscope.Middleware(mux, mcpscope.Options{})
	_ = http.ListenAndServe(":8080", handler)
}
