package mcp

import (
	"context"
	"encoding/json"
)

// Handler implements an iris MCP tool's behavior. The runtime decodes the
// callback envelope and calls Handle with the parsed input. Return a
// Response with IsError=true to surface a tool error to the caller.
type Handler interface {
	Handle(ctx context.Context, input json.RawMessage) Response
}

// HandlerFunc adapts an anonymous function to the Handler interface.
type HandlerFunc func(ctx context.Context, input json.RawMessage) Response

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, input json.RawMessage) Response {
	return f(ctx, input)
}
