package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

type recordedCall struct {
	method string
	params json.RawMessage
}

type fakeExtension struct {
	mu      sync.Mutex
	calls   []recordedCall
	nextTab int
}

func (f *fakeExtension) Send(_ context.Context, method string, params any) (json.RawMessage, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{method: method, params: b})
	defer f.mu.Unlock()
	switch method {
	case "chrome.tabs.create":
		f.nextTab++
		return mustJSON(map[string]any{
			"id":       f.nextTab,
			"index":    0,
			"windowId": 1,
			"url":      "about:blank",
			"active":   true,
			"pinned":   false,
		}), nil
	case "chrome.debugger.attach", "chrome.tabs.remove":
		return json.RawMessage(`{}`), nil
	case "chrome.debugger.sendCommand":
		var args []json.RawMessage
		if err := json.Unmarshal(b, &args); err != nil {
			return nil, err
		}
		var command string
		if len(args) >= 2 {
			_ = json.Unmarshal(args[1], &command)
		}
		if command == "Target.getTargetInfo" {
			var target struct {
				TabID int `json:"tabId"`
			}
			_ = json.Unmarshal(args[0], &target)
			return mustJSON(map[string]any{"targetInfo": map[string]any{
				"targetId":         fmt.Sprintf("target-%d", target.TabID),
				"type":             "page",
				"title":            "",
				"url":              "about:blank",
				"attached":         true,
				"browserContextId": "default",
			}}), nil
		}
		return json.RawMessage(`{}`), nil
	default:
		return nil, fmt.Errorf("unexpected extension method %s", method)
	}
}

func (f *fakeExtension) snapshotCalls() []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedCall(nil), f.calls...)
}

func TestChromedpTargetLifecycleAndChildRouting(t *testing.T) {
	extension := &fakeExtension{nextTab: 40}
	handler := NewHandler()
	if err := handler.SetExtension(extension); err != nil {
		t.Fatal(err)
	}
	var eventsMu sync.Mutex
	var events []CDPMessage
	if err := handler.SetCDPEmitter(func(message CDPMessage) error {
		eventsMu.Lock()
		events = append(events, message)
		eventsMu.Unlock()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	create := handler.HandleCDP(context.Background(), CDPMessage{
		ID:     1,
		Method: "Target.createTarget",
		Params: json.RawMessage(`{"url":"about:blank"}`),
	})
	if create.Error != nil {
		t.Fatalf("create target: %v", create.Error)
	}
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(create.Result, &created); err != nil {
		t.Fatal(err)
	}
	if created.TargetID != "target-41" {
		t.Fatalf("target id = %q", created.TargetID)
	}

	eventsMu.Lock()
	if len(events) != 1 || events[0].Method != "Target.attachedToTarget" {
		t.Fatalf("attached events = %#v", events)
	}
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(events[0].Params, &attached); err != nil {
		t.Fatal(err)
	}
	eventsMu.Unlock()
	if attached.SessionID != "pw-tab-1" {
		t.Fatalf("session id = %q", attached.SessionID)
	}

	attach := handler.HandleCDP(context.Background(), CDPMessage{
		ID:     2,
		Method: "Target.attachToTarget",
		Params: mustJSON(map[string]any{"targetId": created.TargetID, "flatten": true}),
	})
	if attach.Error != nil {
		t.Fatalf("attach target: %v", attach.Error)
	}
	var attachedResult struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(attach.Result, &attachedResult)
	if attachedResult.SessionID != "pw-tab-1" {
		t.Fatalf("attached result = %#v", attachedResult)
	}

	childParams := mustJSON([]any{
		map[string]any{"tabId": 41},
		"Target.attachedToTarget",
		map[string]any{
			"sessionId":          "child-1",
			"targetInfo":         map[string]any{"targetId": "worker-1", "type": "worker"},
			"waitingForDebugger": false,
		},
	})
	if err := handler.HandleExtensionEvent("chrome.debugger.onEvent", childParams); err != nil {
		t.Fatal(err)
	}

	childCommand := handler.HandleCDP(context.Background(), CDPMessage{
		ID:        3,
		SessionID: "child-1",
		Method:    "Runtime.enable",
		Params:    json.RawMessage(`{}`),
	})
	if childCommand.Error != nil {
		t.Fatalf("child command: %v", childCommand.Error)
	}
	calls := extension.snapshotCalls()
	last := calls[len(calls)-1]
	if last.method != "chrome.debugger.sendCommand" {
		t.Fatalf("last method = %q", last.method)
	}
	var args []json.RawMessage
	_ = json.Unmarshal(last.params, &args)
	var target struct {
		TabID     int    `json:"tabId"`
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(args[0], &target)
	if target.TabID != 41 || target.SessionID != "child-1" {
		t.Fatalf("child target = %#v", target)
	}

	beforeDetach := len(calls)
	detach := handler.HandleCDP(context.Background(), CDPMessage{
		ID:     4,
		Method: "Target.detachFromTarget",
		Params: json.RawMessage(`{"sessionId":"pw-tab-1"}`),
	})
	if detach.Error != nil {
		t.Fatalf("detach: %v", detach.Error)
	}
	if got := len(extension.snapshotCalls()); got != beforeDetach {
		t.Fatalf("synthetic detach forwarded to extension: calls %d -> %d", beforeDetach, got)
	}

	discover := handler.HandleCDP(context.Background(), CDPMessage{
		ID:        5,
		SessionID: "pw-tab-1",
		Method:    "Target.setDiscoverTargets",
		Params:    json.RawMessage(`{"discover":true}`),
	})
	if discover.Error != nil {
		t.Fatalf("set discover targets: %v", discover.Error)
	}
	if got := len(extension.snapshotCalls()); got != beforeDetach {
		t.Fatalf("setDiscoverTargets forwarded to extension: calls %d -> %d", beforeDetach, got)
	}
}

func TestExtensionInitializationHandshake(t *testing.T) {
	handler := NewHandler()
	if handler.IsReady() {
		t.Fatal("handler unexpectedly ready")
	}
	if err := handler.HandleExtensionEvent("extension.initialized", json.RawMessage(`[]`)); err != nil {
		t.Fatal(err)
	}
	if !handler.IsReady() {
		t.Fatal("handler did not become ready")
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
