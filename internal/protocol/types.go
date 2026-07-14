package protocol

import (
	"context"
	"encoding/json"
)

// ExtensionSender sends a command over the Extension Protocol V2 connection.
type ExtensionSender interface {
	Send(context.Context, string, any) (json.RawMessage, error)
}

// CDPMessage is a flat Chrome DevTools Protocol message.
type CDPMessage struct {
	ID        int64           `json:"id,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *CDPError       `json:"error,omitempty"`
}

type CDPError struct {
	Code    int64  `json:"code"`
	Message string `json:"message"`
}

type Tab struct {
	ID          *int   `json:"id"`
	Index       int    `json:"index"`
	WindowID    int    `json:"windowId"`
	OpenerTabID *int   `json:"openerTabId,omitempty"`
	URL         string `json:"url,omitempty"`
	Title       string `json:"title,omitempty"`
	Active      bool   `json:"active"`
	Pinned      bool   `json:"pinned"`
}

type debuggerSource struct {
	TabID     *int   `json:"tabId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}
