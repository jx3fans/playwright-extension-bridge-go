[**中文**](README.zh-CN.md)

# Playwright Extension Bridge for Go

A Go library that controls Chromium-based browsers (Edge, Chrome) through the [Playwright Extension](https://chromewebstore.google.com/detail/playwright-extension/mmlmfjhmonkocbjadbfplnigmagldckm) CDP bridge — no Node.js, no Playwright driver, no `--remote-debugging-port`.

```go
import "github.com/jx3fans/playwright-extension-bridge-go"
```

## How it works

```
Go program  ──→  loopback WebSocket relay  ──→  Playwright Extension  ──→  Edge/Chrome
                     │
                     └──→ chromedp (CDP client)
```

The extension relays `chrome.debugger` / `chrome.tabs` API calls via Extension Protocol V2.  The Go side speaks CDP through `chromedp` with synthetic target lifecycle management.

## Requirements

- Edge or Chrome with [Playwright Extension 0.2.1](https://chromewebstore.google.com/detail/playwright-extension/mmlmfjhmonkocbjadbfplnigmagldckm) installed
- macOS or Linux

## Quick start

```go
ctx := context.Background()
bridge, err := pebridge.New(ctx, pebridge.Options{
    ExtensionToken: "<32-byte-base64url-token>",
})
if err != nil {
    log.Fatal(err)  // check Edge profile, extension install, or token mismatch
}
defer bridge.Close()

page, err := bridge.NewPage(ctx, "https://example.com")
if err != nil {
    log.Fatal(err)
}
defer page.Close()
```

### Get a token

1. Open `chrome-extension://mmlmfjhmonkocbjadbfplnigmagldckm/connect.html` in the target browser.
2. Look in DevTools → Application → Local Storage for the `token` key.
3. Paste the value into `ExtensionToken`.

With a valid token the extension skips the Allow UI and connects automatically.

### MCP-style tools

```go
bridge.BrowserNavigate(ctx, pebridge.NavigateParams{URL: "https://example.com"})
bridge.BrowserClick(ctx, pebridge.ClickParams{Target: "#submit"})
bridge.BrowserType(ctx, pebridge.TypeParams{Target: "#search", Text: "query"})

result, _ := bridge.BrowserEvaluate(ctx, pebridge.EvaluateParams{
    Function: `() => document.title`,
})
snapshot, _ := bridge.BrowserSnapshot(ctx, pebridge.SnapshotParams{Depth: 3})
data, _ := bridge.BrowserTakeScreenshot(ctx, pebridge.TakeScreenshotParams{
    Type: "png", Scale: "css",
})
bridge.BrowserTabs(ctx, pebridge.TabsParams{Action: "list"})
```

See `mcp_tools.go` for all 24 tools and their parameters.

## Limitations

This library speaks CDP directly — it is **not** a Playwright API wrapper. Key differences from the Playwright MCP server:

| Tool | Limitation |
|---|---|
| `browser_snapshot` | Uses CDP `Accessibility.getFullAXTree`. This is a PoC-level approximation of the Playwright MCP snapshot — the tree is less rich, and iframe/text distillation logic is not implemented. |
| `browser_run_code_unsafe` | Takes a **Go callback** (`func(context.Context, *Page) (any, error)`), not a JavaScript string. To run arbitrary JS, use `BrowserEvaluate` inside the callback. |
| `browser_wait_for` | Only supports wall-clock `time` (capped 30s) and `text`/`textGone` polling via `innerText.includes`. No Playwright Locator auto-wait semantics (visibility, stability, enabled). |
| `browser_click`, `browser_type`, `browser_fill_form` | No built-in actionability checks. CDP executes the action immediately regardless of element visibility or stability. |
| `browser_drag` / `browser_drop` | Uses manual CDP mouse event dispatch. May not work with frameworks that rely on the HTML5 Drag and Drop API. |
| `browser_handle_dialog` | Dialog state is captured via CDP event listeners set up at page creation. Dialogs that fire before the page is created are not captured. |

All other tools (`navigate`, `navigate_back`, `evaluate`, `screenshot`, `tabs`, `press_key`, `select_option`, `file_upload`, `console_messages`, `network_requests`, `network_request`, `find`, `hover`, `resize`, `close`) are functionally complete.

## Version

`go 1.26`

## License

MIT
