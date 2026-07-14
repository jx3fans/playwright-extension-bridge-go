package pebridge

import (
	"reflect"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp/kb"
)

func TestMCPToolNames(t *testing.T) {
	want := []string{
		"browser_close", "browser_resize", "browser_console_messages",
		"browser_handle_dialog", "browser_evaluate", "browser_file_upload",
		"browser_drop", "browser_find", "browser_fill_form", "browser_press_key",
		"browser_type", "browser_navigate", "browser_navigate_back",
		"browser_network_requests", "browser_network_request", "browser_run_code_unsafe",
		"browser_take_screenshot", "browser_snapshot", "browser_click", "browser_drag",
		"browser_hover", "browser_select_option", "browser_tabs", "browser_wait_for",
	}
	if got := MCPToolNames(); !reflect.DeepEqual(got, want) {
		t.Fatalf("MCPToolNames() = %#v, want %#v", got, want)
	}
	got := MCPToolNames()
	got[0] = "mutated"
	if MCPToolNames()[0] != ToolBrowserClose {
		t.Fatal("MCPToolNames returned mutable package state")
	}
}

func TestCompileToolRegex(t *testing.T) {
	tests := []struct {
		source string
		match  string
		want   bool
	}{
		{source: "/error/i", match: "ERROR", want: true},
		{source: "^api/", match: "api/items", want: true},
		{source: "/foo/", match: "FOO", want: false},
	}
	for _, test := range tests {
		re, err := compileToolRegex(test.source)
		if err != nil {
			t.Fatalf("compileToolRegex(%q): %v", test.source, err)
		}
		if got := re.MatchString(test.match); got != test.want {
			t.Errorf("compileToolRegex(%q).MatchString(%q) = %v, want %v", test.source, test.match, got, test.want)
		}
	}
	if _, err := compileToolRegex("/[a-/"); err == nil {
		t.Fatal("compileToolRegex accepted invalid expression")
	}
}

func TestEncodeKey(t *testing.T) {
	key, modifiers, err := encodeKey("Control+Shift+ArrowLeft")
	if err != nil {
		t.Fatal(err)
	}
	if key != kb.ArrowLeft {
		t.Fatalf("key = %q, want ArrowLeft encoding", key)
	}
	if !reflect.DeepEqual(modifiers, []input.Modifier{input.ModifierCtrl, input.ModifierShift}) {
		t.Fatalf("modifiers = %#v", modifiers)
	}
	if _, _, err := encodeKey("Hyper+K"); err == nil {
		t.Fatal("encodeKey accepted unsupported modifier")
	}
}

func TestRenderNetworkRequest(t *testing.T) {
	request := &NetworkRequest{
		URL:            "https://example.com/api/items",
		Method:         "post",
		ResourceType:   "XHR",
		RequestHeaders: map[string]string{"content-type": "application/json"},
		HasPostData:    true,
		Response: &NetworkResponse{
			Status:     201,
			StatusText: "Created",
			Headers:    map[string]string{"content-type": "application/json"},
			MimeType:   "application/json",
		},
	}
	got := renderNetworkRequest(2, request)
	for _, want := range []string{"#2 [POST]", "[201] Created", "browser_network_request", `part="response-body"`} {
		if !strings.Contains(got, want) {
			t.Errorf("renderNetworkRequest output missing %q:\n%s", want, got)
		}
	}
}
