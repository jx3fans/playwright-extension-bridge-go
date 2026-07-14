package browser

import (
	"net/url"
	"strings"
	"testing"
)

func TestConnectURL(t *testing.T) {
	raw, err := ConnectURL(LaunchOptions{
		RelayEndpoint:  "ws://127.0.0.1:1234/extension/id",
		ExtensionToken: "secret+/= value",
		ClientName:     "test-client",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "chrome-extension" || u.Host != ExtensionID || u.Path != "/connect.html" {
		t.Fatalf("unexpected connect URL: %s", raw)
	}
	query := u.Query()
	if query.Get("mcpRelayUrl") != "ws://127.0.0.1:1234/extension/id" {
		t.Fatalf("relay URL = %q", query.Get("mcpRelayUrl"))
	}
	if query.Get("token") != "secret+/= value" {
		t.Fatalf("token did not round trip")
	}
	if query.Get("protocolVersion") != "2" {
		t.Fatalf("protocol version = %q", query.Get("protocolVersion"))
	}
	if !strings.Contains(query.Get("client"), "test-client") {
		t.Fatalf("client = %q", query.Get("client"))
	}
}
