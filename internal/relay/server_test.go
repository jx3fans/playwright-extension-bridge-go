package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/jx3fans/playwright-extension-bridge-go/internal/protocol"
)

func TestServerHandshakeAndBrowserVersion(t *testing.T) {
	handler := protocol.NewHandler()
	server, err := NewServer(handler)
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	extension, _, err := websocket.Dial(ctx, server.ExtensionEndpoint(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer extension.CloseNow()
	initialized, _ := json.Marshal(map[string]any{"method": "extension.initialized", "params": []any{}})
	if err := extension.Write(ctx, websocket.MessageText, initialized); err != nil {
		t.Fatal(err)
	}
	if err := handler.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	cdp, _, err := websocket.Dial(ctx, server.CDPEndpoint(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cdp.CloseNow()
	request := []byte(`{"id":1,"method":"Browser.getVersion","params":{}}`)
	if err := cdp.Write(ctx, websocket.MessageText, request); err != nil {
		t.Fatal(err)
	}
	_, data, err := cdp.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var response protocol.CDPMessage
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != 1 || response.Error != nil {
		t.Fatalf("unexpected response: %s", data)
	}
	var version struct {
		ProtocolVersion string `json:"protocolVersion"`
		Product         string `json:"product"`
	}
	if err := json.Unmarshal(response.Result, &version); err != nil {
		t.Fatal(err)
	}
	if version.ProtocolVersion != "1.3" || version.Product != "Chrome/Extension-Bridge" {
		t.Fatalf("version = %#v", version)
	}
}

func TestServerCreateAndAttachTargetThroughExtension(t *testing.T) {
	handler := protocol.NewHandler()
	server, err := NewServer(handler)
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Close(ctx)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	extension, _, err := websocket.Dial(ctx, server.ExtensionEndpoint(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer extension.CloseNow()

	responderDone := make(chan error, 1)
	go func() {
		for {
			_, data, err := extension.Read(ctx)
			if err != nil {
				responderDone <- err
				return
			}
			var command struct {
				ID     int64             `json:"id"`
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(data, &command); err != nil {
				responderDone <- err
				return
			}
			var result any = map[string]any{}
			switch command.Method {
			case "chrome.tabs.create":
				result = map[string]any{"id": 9, "index": 0, "windowId": 1, "url": "about:blank", "active": true, "pinned": false}
			case "chrome.debugger.attach":
			case "chrome.debugger.sendCommand":
				var method string
				if len(command.Params) > 1 {
					_ = json.Unmarshal(command.Params[1], &method)
				}
				if method != "Target.getTargetInfo" {
					responderDone <- fmt.Errorf("unexpected CDP command %s", method)
					return
				}
				result = map[string]any{"targetInfo": map[string]any{
					"targetId": "target-9", "type": "page", "title": "", "url": "about:blank", "browserContextId": "default",
				}}
			default:
				responderDone <- fmt.Errorf("unexpected extension command %s", command.Method)
				return
			}
			response, _ := json.Marshal(map[string]any{"id": command.ID, "result": result})
			if err := extension.Write(ctx, websocket.MessageText, response); err != nil {
				responderDone <- err
				return
			}
		}
	}()

	initialized, _ := json.Marshal(map[string]any{"method": "extension.initialized", "params": []any{}})
	if err := extension.Write(ctx, websocket.MessageText, initialized); err != nil {
		t.Fatal(err)
	}
	if err := handler.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}

	cdp, _, err := websocket.Dial(ctx, server.CDPEndpoint(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cdp.CloseNow()
	if err := cdp.Write(ctx, websocket.MessageText, []byte(`{"id":1,"method":"Target.createTarget","params":{"url":"about:blank"}}`)); err != nil {
		t.Fatal(err)
	}

	var attached protocol.CDPMessage
	var created protocol.CDPMessage
	for attached.Method == "" || created.ID == 0 {
		_, data, err := cdp.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var message protocol.CDPMessage
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Method == "Target.attachedToTarget" {
			attached = message
		}
		if message.ID == 1 {
			created = message
		}
	}
	if created.Error != nil || string(created.Result) != `{"targetId":"target-9"}` {
		t.Fatalf("create response = %+v, result=%s", created, created.Result)
	}
	var attachedParams struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(attached.Params, &attachedParams); err != nil {
		t.Fatal(err)
	}
	if attachedParams.SessionID != "pw-tab-1" {
		t.Fatalf("attached session = %q", attachedParams.SessionID)
	}

	attachRequest := []byte(`{"id":2,"method":"Target.attachToTarget","params":{"targetId":"target-9","flatten":true}}`)
	if err := cdp.Write(ctx, websocket.MessageText, attachRequest); err != nil {
		t.Fatal(err)
	}
	_, data, err := cdp.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var attachResponse protocol.CDPMessage
	if err := json.Unmarshal(data, &attachResponse); err != nil {
		t.Fatal(err)
	}
	if attachResponse.Error != nil || string(attachResponse.Result) != `{"sessionId":"pw-tab-1"}` {
		t.Fatalf("attach response = %+v, result=%s", attachResponse, attachResponse.Result)
	}

	_ = cdp.Close(websocket.StatusNormalClosure, "done")
	select {
	case <-responderDone:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
