package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/coder/websocket"
)

var ErrConnectionClosed = errors.New("websocket connection closed")

type extensionResponse struct {
	result json.RawMessage
	err    error
}

type extensionWireMessage struct {
	ID     *int64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

// ExtensionConn pairs Extension Protocol V2 request ids with responses.
type ExtensionConn struct {
	conn *websocket.Conn

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan extensionResponse
	closed  bool
	done    chan struct{}

	onEvent func(string, json.RawMessage) error
	onClose func(error)
}

func NewExtensionConn(conn *websocket.Conn, onEvent func(string, json.RawMessage) error, onClose func(error)) *ExtensionConn {
	return &ExtensionConn{
		conn:    conn,
		pending: make(map[int64]chan extensionResponse),
		done:    make(chan struct{}),
		onEvent: onEvent,
		onClose: onClose,
	}
}

func (c *ExtensionConn) Run(ctx context.Context) error {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.shutdown(fmt.Errorf("read extension websocket: %w", err))
			return err
		}
		var msg extensionWireMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			c.shutdown(fmt.Errorf("decode extension message: %w", err))
			return err
		}
		if msg.ID != nil {
			c.deliverResponse(*msg.ID, msg)
			continue
		}
		if msg.Method == "" {
			continue
		}
		if err := c.onEvent(msg.Method, msg.Params); err != nil {
			c.shutdown(fmt.Errorf("handle extension event %s: %w", msg.Method, err))
			return err
		}
	}
}

func (c *ExtensionConn) Send(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrConnectionClosed
	}
	c.nextID++
	id := c.nextID
	result := make(chan extensionResponse, 1)
	c.pending[id] = result
	c.mu.Unlock()

	payload, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		c.removePending(id)
		return nil, err
	}
	if err := c.write(ctx, payload); err != nil {
		c.removePending(id)
		c.shutdown(fmt.Errorf("write extension websocket: %w", err))
		return nil, err
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrConnectionClosed
	case response := <-result:
		return response.result, response.err
	}
}

func (c *ExtensionConn) Close(status websocket.StatusCode, reason string) error {
	err := c.conn.Close(status, reason)
	c.shutdown(ErrConnectionClosed)
	return err
}

func (c *ExtensionConn) write(ctx context.Context, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func (c *ExtensionConn) deliverResponse(id int64, msg extensionWireMessage) {
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch == nil {
		return
	}
	var response extensionResponse
	if len(msg.Error) != 0 && string(msg.Error) != "null" {
		var text string
		if err := json.Unmarshal(msg.Error, &text); err == nil {
			response.err = errors.New(text)
		} else {
			response.err = fmt.Errorf("extension error: %s", msg.Error)
		}
	} else {
		response.result = msg.Result
		if len(response.result) == 0 {
			response.result = json.RawMessage(`{}`)
		}
	}
	ch <- response
}

func (c *ExtensionConn) removePending(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *ExtensionConn) shutdown(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		ch <- extensionResponse{err: err}
		delete(c.pending, id)
	}
	close(c.done)
	c.mu.Unlock()
	if c.onClose != nil {
		c.onClose(err)
	}
}
