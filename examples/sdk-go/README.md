# SDK Go Example

Minimal HTTP middleware usage for reporting traces to a running mcpscope instance.

```go
handler := mcpscope.Middleware(appHandler, mcpscope.Options{})
http.ListenAndServe(":8080", handler)
```
