package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrExtensionDisconnected = errors.New("playwright extension disconnected")

const extensionCommandTimeout = 30 * time.Second

// Handler translates Extension Protocol V2 events and flat CDP commands.
type Handler struct {
	mu sync.RWMutex

	sender ExtensionSender
	emit   func(CDPMessage) error

	ready     chan struct{}
	readyOnce sync.Once
	done      chan struct{}
	doneOnce  sync.Once
	doneErr   error

	model *BrowserModel
}

func NewHandler() *Handler {
	h := &Handler{
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}
	h.model = NewBrowserModel(func(ctx context.Context, method string, params any) (json.RawMessage, error) {
		h.mu.RLock()
		sender := h.sender
		h.mu.RUnlock()
		if sender == nil {
			return nil, ErrExtensionDisconnected
		}
		commandCtx, cancel := context.WithTimeout(ctx, extensionCommandTimeout)
		defer cancel()
		return sender.Send(commandCtx, method, params)
	}, h.emitCDP)
	return h
}

func (h *Handler) SetExtension(sender ExtensionSender) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.sender != nil {
		return errors.New("another extension connection is already active")
	}
	h.sender = sender
	return nil
}

func (h *Handler) ExtensionDisconnected(err error) {
	if err == nil {
		err = ErrExtensionDisconnected
	}
	h.mu.Lock()
	h.sender = nil
	h.mu.Unlock()
	h.doneOnce.Do(func() {
		h.mu.Lock()
		h.doneErr = err
		h.mu.Unlock()
		close(h.done)
	})
}

func (h *Handler) SetCDPEmitter(emit func(CDPMessage) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.emit != nil {
		return errors.New("another CDP client is already active")
	}
	h.emit = emit
	return nil
}

func (h *Handler) ClearCDPEmitter() {
	h.mu.Lock()
	h.emit = nil
	h.mu.Unlock()
}

func (h *Handler) Ready() <-chan struct{} { return h.ready }

func (h *Handler) Done() <-chan struct{} { return h.done }

func (h *Handler) Err() error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.doneErr
}

func (h *Handler) IsReady() bool {
	select {
	case <-h.done:
		return false
	default:
	}
	select {
	case <-h.ready:
		return true
	default:
		return false
	}
}

func (h *Handler) WaitReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return h.Err()
	case <-h.ready:
		return nil
	}
}

func (h *Handler) HandleExtensionEvent(method string, params json.RawMessage) error {
	switch method {
	case "chrome.debugger.onEvent":
		var args []json.RawMessage
		if err := json.Unmarshal(params, &args); err != nil {
			return fmt.Errorf("decode debugger event: %w", err)
		}
		if len(args) < 2 {
			return errors.New("decode debugger event: expected at least 2 arguments")
		}
		var source debuggerSource
		var cdpMethod string
		if err := json.Unmarshal(args[0], &source); err != nil {
			return err
		}
		if err := json.Unmarshal(args[1], &cdpMethod); err != nil {
			return err
		}
		var cdpParams json.RawMessage
		if len(args) > 2 {
			cdpParams = args[2]
		}
		h.model.OnDebuggerEvent(source, cdpMethod, cdpParams)
	case "chrome.debugger.onDetach":
		var args []json.RawMessage
		if err := json.Unmarshal(params, &args); err != nil {
			return fmt.Errorf("decode debugger detach: %w", err)
		}
		if len(args) == 0 {
			return errors.New("decode debugger detach: expected source argument")
		}
		var source debuggerSource
		if err := json.Unmarshal(args[0], &source); err != nil {
			return err
		}
		h.model.OnDebuggerDetach(source)
	case "chrome.tabs.onCreated":
		var args []json.RawMessage
		if err := json.Unmarshal(params, &args); err != nil {
			return fmt.Errorf("decode tab created: %w", err)
		}
		if len(args) == 0 {
			return errors.New("decode tab created: expected tab argument")
		}
		var tab Tab
		if err := json.Unmarshal(args[0], &tab); err != nil {
			return err
		}
		h.model.OnTabCreated(tab)
	case "chrome.tabs.onRemoved":
		var args []json.RawMessage
		if err := json.Unmarshal(params, &args); err != nil {
			return fmt.Errorf("decode tab removed: %w", err)
		}
		if len(args) == 0 {
			return errors.New("decode tab removed: expected tab id argument")
		}
		var tabID int
		if err := json.Unmarshal(args[0], &tabID); err != nil {
			return err
		}
		h.model.OnTabRemoved(tabID)
	case "extension.initialized":
		h.readyOnce.Do(func() { close(h.ready) })
	}
	return nil
}

func (h *Handler) HandleCDP(ctx context.Context, req CDPMessage) CDPMessage {
	resp := CDPMessage{ID: req.ID, SessionID: req.SessionID}
	result, err := h.handleCDP(ctx, req)
	if err != nil {
		resp.Error = &CDPError{Code: -32000, Message: err.Error()}
		return resp
	}
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	resp.Result = result
	return resp
}

func (h *Handler) handleCDP(ctx context.Context, req CDPMessage) (json.RawMessage, error) {
	switch req.Method {
	case "Browser.getVersion":
		return marshalRaw(map[string]any{
			"protocolVersion": "1.3",
			"product":         "Chrome/Extension-Bridge",
			"revision":        "",
			"userAgent":       "playwright-extension-bridge-go/1.0",
			"jsVersion":       "",
		})
	case "Browser.setDownloadBehavior":
		return json.RawMessage(`{}`), nil
	case "Target.setAutoAttach":
		if req.SessionID == "" {
			if err := h.model.EnableAutoAttach(ctx); err != nil {
				return nil, err
			}
			return json.RawMessage(`{}`), nil
		}
	case "Target.createTarget":
		var p struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return h.model.CreateTarget(ctx, p.URL)
	case "Target.closeTarget":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return h.model.CloseTarget(ctx, p.TargetID)
	case "Target.attachToTarget":
		if req.SessionID == "" {
			var p struct {
				TargetID string `json:"targetId"`
			}
			_ = json.Unmarshal(req.Params, &p)
			return h.model.AttachToTarget(ctx, p.TargetID)
		}
	case "Target.detachFromTarget":
		if req.SessionID == "" {
			var p struct {
				SessionID string `json:"sessionId"`
			}
			_ = json.Unmarshal(req.Params, &p)
			if h.model.IsTopLevelSession(p.SessionID) {
				return json.RawMessage(`{}`), nil
			}
		}
	case "Target.getTargetInfo":
		var p struct {
			TargetID string `json:"targetId"`
		}
		_ = json.Unmarshal(req.Params, &p)
		return h.model.GetTargetInfo(req.SessionID, p.TargetID)
	case "Target.getTargets":
		if req.SessionID == "" {
			return h.model.GetTargets()
		}
	case "Target.setDiscoverTargets":
		if req.SessionID == "" {
			h.model.EmitTargetCreatedEvents()
		}
		// chrome.debugger rejects this command on a tab target with "Not allowed".
		// chromedp only uses it for discovery bookkeeping; BrowserModel already
		// owns top-level target discovery and child targets use setAutoAttach.
		return json.RawMessage(`{}`), nil
	}
	return h.model.Forward(ctx, req.SessionID, req.Method, req.Params)
}

func (h *Handler) emitCDP(msg CDPMessage) {
	h.mu.RLock()
	emit := h.emit
	h.mu.RUnlock()
	if emit != nil {
		_ = emit(msg)
	}
}

func marshalRaw(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}
