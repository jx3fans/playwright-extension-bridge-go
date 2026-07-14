# Playwright Extension Bridge for Go

通过 [Playwright Extension](https://chromewebstore.google.com/detail/playwright-extension/mmlmfjhmonkocbjadbfplnigmagldckm) 的 CDP 桥接控制 Chromium 内核浏览器（Edge/Chrome）的 Go 库 —— 无需 Node.js、无需 Playwright 驱动、无需 `--remote-debugging-port`。

```go
import "github.com/jx3fans/playwright-extension-bridge-go"
```

## 工作原理

```
Go 程序  ──→  loopback WebSocket 中转  ──→  Playwright Extension  ──→  Edge/Chrome
                   │
                   └──→ chromedp (CDP 客户端)
```

Extension 通过 Extension Protocol V2 中转 `chrome.debugger` / `chrome.tabs` API 调用。Go 侧通过 `chromedp` 与 CDP 通信，并管理合成目标生命周期。

## 环境要求

- Edge 或 Chrome 已安装 [Playwright Extension 0.2.1](https://chromewebstore.google.com/detail/playwright-extension/mmlmfjhmonkocbjadbfplnigmagldckm)
- macOS 或 Linux

## 快速开始

```go
ctx := context.Background()
bridge, err := pebridge.New(ctx, pebridge.Options{
    ExtensionToken: "<32-byte-base64url-token>",
})
if err != nil {
    log.Fatal(err)  // 检查浏览器 profile、扩展安装、或 token 是否匹配
}
defer bridge.Close()

page, err := bridge.NewPage(ctx, "https://example.com")
if err != nil {
    log.Fatal(err)
}
defer page.Close()
```

### 获取 Token

1. 在目标浏览器中打开 `chrome-extension://mmlmfjhmonkocbjadbfplnigmagldckm/connect.html`
2. 打开 DevTools → Application → Local Storage，找到 `token` 键
3. 将值复制到 `ExtensionToken` 字段

设置有效 token 后，扩展将跳过授权确认界面，自动建立连接。

### MCP 风格工具

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

所有 24 个工具及其参数详见 `mcp_tools.go`。

## 已知限制

本库直接通过 CDP 通信，**并非** Playwright API 的封装。与 Playwright MCP 服务器的关键差异如下：

| 工具 | 限制 |
|---|---|
| `browser_snapshot` | 使用 CDP `Accessibility.getFullAXTree`，是 Playwright MCP snapshot 的 PoC 级近似版本——树结构不够丰富，未实现 iframe 和文本蒸馏逻辑。 |
| `browser_run_code_unsafe` | 接收 **Go 回调** (`func(context.Context, *Page) (any, error)`)，而非 JavaScript 字符串。如需执行任意 JS，请在回调内使用 `BrowserEvaluate`。 |
| `browser_wait_for` | 仅支持墙钟 `time`（上限 30 秒）和通过 `innerText.includes` 轮询 `text`/`textGone`。不具备 Playwright Locator 的自动等待语义（可见性、稳定性、启用状态）。 |
| `browser_click`、`browser_type`、`browser_fill_form` | 无内置可交互性检查。CDP 会立即执行动作，不关心元素是否可见或稳定。 |
| `browser_drag` / `browser_drop` | 使用手动 CDP 鼠标事件分发。可能无法与依赖 HTML5 拖放 API 的框架配合使用。 |
| `browser_handle_dialog` | 对话框状态通过页面创建时注册的 CDP 事件监听器捕获。在页面创建之前弹出的对话框无法捕获。 |

其余工具（`navigate`、`navigate_back`、`evaluate`、`screenshot`、`tabs`、`press_key`、`select_option`、`file_upload`、`console_messages`、`network_requests`、`network_request`、`find`、`hover`、`resize`、`close`）功能完整。

## 版本要求

`go 1.26`

## 许可证

MIT
