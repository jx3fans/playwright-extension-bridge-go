package pebridge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestMCPToolsEdgeIntegration(t *testing.T) {
	if os.Getenv("PECDP_E2E") != "1" {
		t.Skip("set PECDP_E2E=1 to run the real Edge integration test")
	}
	token := extensionTokenForTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	browser, err := New(ctx, Options{ExtensionToken: token, ConnectTimeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	t.Log("browser connected")

	html := `<!doctype html><html><body>
<label>Name <input id="name"></label>
<label><input id="enabled" type="checkbox"> Enabled</label>
<select id="color"><option>Red</option><option>Blue</option></select>
<button id="submit" onclick="document.querySelector('#result').textContent='submitted:'+document.querySelector('#name').value;console.log('submitted')">Submit</button>
<button id="dialog" onclick="alert('hello')">Dialog</button>
<input id="upload" type="file">
<div id="drop" ondragover="event.preventDefault()" ondrop="event.preventDefault();this.textContent=event.dataTransfer.getData('text/plain')">Drop</div>
<div id="drag" style="width:40px;height:40px;background:red" onmousedown="window.dragged=true"></div>
<div id="drag-target" style="width:40px;height:40px;background:blue" onmouseup="this.textContent=window.dragged?'dragged':'no'">Target</div>
<div id="result"></div>
</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, html)
	}))
	defer server.Close()
	pageURL := server.URL
	if err := browser.BrowserNavigate(ctx, NavigateParams{URL: pageURL}); err != nil {
		t.Fatal(err)
	}
	t.Log("navigation complete")
	if err := browser.BrowserResize(ctx, ResizeParams{Width: 900, Height: 700}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserFillForm(ctx, FillFormParams{Fields: []FormField{
		{Name: "name", Type: "textbox", Target: "#name", Value: "alpha"},
		{Name: "enabled", Type: "checkbox", Target: "#enabled", Value: "true"},
		{Name: "color", Type: "combobox", Target: "#color", Value: "Blue"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserType(ctx, TypeParams{Target: "#name", Text: "beta"}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserHover(ctx, HoverParams{Target: "#submit"}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserClick(ctx, ClickParams{Target: "#submit"}); err != nil {
		t.Fatal(err)
	}
	t.Log("form interaction complete")
	value, err := browser.BrowserEvaluate(ctx, EvaluateParams{Function: `() => document.querySelector('#result').textContent`})
	if err != nil || value != "submitted:beta" {
		t.Fatalf("evaluate = %#v, %v", value, err)
	}
	t.Log("wait and evaluate complete")
	snapshot, err := browser.BrowserSnapshot(ctx, SnapshotParams{})
	if err != nil || !strings.Contains(snapshot, "Submit") || !strings.Contains(snapshot, "[ref=e") {
		t.Fatalf("snapshot missing expected content: %v\n%s", err, snapshot)
	}
	if found, err := browser.BrowserFind(ctx, FindParams{Text: "Submit"}); err != nil || !strings.Contains(found, "Found") {
		t.Fatalf("find = %q, %v", found, err)
	}
	if _, err := browser.BrowserTakeScreenshot(ctx, TakeScreenshotParams{Target: "#submit", Type: "png", Scale: "css"}); err != nil {
		t.Fatal(err)
	}
	t.Log("snapshot, find and screenshot complete")
	if err := browser.BrowserDrop(ctx, DropParams{Target: "#drop", Data: map[string]string{"text/plain": "dropped"}}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserWaitFor(ctx, WaitForParams{Text: "dropped"}); err != nil {
		t.Fatal(err)
	}
	t.Log("drop complete")
	if err := browser.BrowserDrag(ctx, DragParams{StartTarget: "#drag", EndTarget: "#drag-target"}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserClick(ctx, ClickParams{Target: "#dialog"}); err != nil {
		t.Fatal(err)
	}
	if err := browser.BrowserHandleDialog(ctx, HandleDialogParams{Accept: true}); err != nil {
		t.Fatal(err)
	}
	t.Log("drag and dialog complete")
	messages, err := browser.BrowserConsoleMessages(ctx, ConsoleMessagesParams{Level: ConsoleInfo})
	if err != nil || !strings.Contains(messages.Text, "submitted") {
		t.Fatalf("console = %#v, %v", messages, err)
	}
	result, err := browser.BrowserRunCodeUnsafe(ctx, func(ctx context.Context, page *Page) (any, error) {
		return page.Title(ctx)
	})
	if err != nil || result == nil {
		t.Fatalf("run code = %#v, %v", result, err)
	}
	t.Log("console and run code complete")
	tabs, err := browser.BrowserTabs(ctx, TabsParams{Action: "new", URL: "about:blank"})
	if err != nil || len(tabs) != 2 {
		t.Fatalf("new tab = %#v, %v", tabs, err)
	}
	index := 0
	if _, err := browser.BrowserTabs(ctx, TabsParams{Action: "select", Index: &index}); err != nil {
		t.Fatal(err)
	}
	index = 1
	if _, err := browser.BrowserTabs(ctx, TabsParams{Action: "close", Index: &index}); err != nil {
		t.Fatal(err)
	}
	t.Log("tabs complete")
}

func extensionTokenForTest(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("cmd/poc/main.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`const\s+extensionToken\s*=\s*"([^"]+)"`)
	match := re.FindSubmatch(data)
	if len(match) != 2 {
		t.Fatal("hardcoded extension token not found in cmd/poc/main.go")
	}
	return string(match[1])
}
