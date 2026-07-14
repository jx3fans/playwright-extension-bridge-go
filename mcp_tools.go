package pebridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Tool names intentionally match Playwright MCP's public core tool names.
const (
	ToolBrowserClose           = "browser_close"
	ToolBrowserResize          = "browser_resize"
	ToolBrowserConsoleMessages = "browser_console_messages"
	ToolBrowserHandleDialog    = "browser_handle_dialog"
	ToolBrowserEvaluate        = "browser_evaluate"
	ToolBrowserFileUpload      = "browser_file_upload"
	ToolBrowserDrop            = "browser_drop"
	ToolBrowserFind            = "browser_find"
	ToolBrowserFillForm        = "browser_fill_form"
	ToolBrowserPressKey        = "browser_press_key"
	ToolBrowserType            = "browser_type"
	ToolBrowserNavigate        = "browser_navigate"
	ToolBrowserNavigateBack    = "browser_navigate_back"
	ToolBrowserNetworkRequests = "browser_network_requests"
	ToolBrowserNetworkRequest  = "browser_network_request"
	ToolBrowserRunCodeUnsafe   = "browser_run_code_unsafe"
	ToolBrowserTakeScreenshot  = "browser_take_screenshot"
	ToolBrowserSnapshot        = "browser_snapshot"
	ToolBrowserClick           = "browser_click"
	ToolBrowserDrag            = "browser_drag"
	ToolBrowserHover           = "browser_hover"
	ToolBrowserSelectOption    = "browser_select_option"
	ToolBrowserTabs            = "browser_tabs"
	ToolBrowserWaitFor         = "browser_wait_for"
)

var mcpToolNames = []string{
	ToolBrowserClose, ToolBrowserResize, ToolBrowserConsoleMessages,
	ToolBrowserHandleDialog, ToolBrowserEvaluate, ToolBrowserFileUpload,
	ToolBrowserDrop, ToolBrowserFind, ToolBrowserFillForm, ToolBrowserPressKey,
	ToolBrowserType, ToolBrowserNavigate, ToolBrowserNavigateBack,
	ToolBrowserNetworkRequests, ToolBrowserNetworkRequest, ToolBrowserRunCodeUnsafe,
	ToolBrowserTakeScreenshot, ToolBrowserSnapshot, ToolBrowserClick,
	ToolBrowserDrag, ToolBrowserHover, ToolBrowserSelectOption, ToolBrowserTabs,
	ToolBrowserWaitFor,
}

func MCPToolNames() []string { return append([]string(nil), mcpToolNames...) }

type ElementTarget struct {
	Element string `json:"element,omitempty"`
	Target  string `json:"target"`
}

type ResizeParams struct {
	Width  int64 `json:"width"`
	Height int64 `json:"height"`
}

type ConsoleLevel string

const (
	ConsoleError   ConsoleLevel = "error"
	ConsoleWarning ConsoleLevel = "warning"
	ConsoleInfo    ConsoleLevel = "info"
	ConsoleDebug   ConsoleLevel = "debug"
)

type ConsoleMessage struct {
	Level      ConsoleLevel `json:"level"`
	Text       string       `json:"text"`
	Timestamp  time.Time    `json:"timestamp"`
	Navigation int64        `json:"-"`
}

type ConsoleMessagesParams struct {
	Level    ConsoleLevel `json:"level,omitempty"`
	All      bool         `json:"all,omitempty"`
	Filename string       `json:"filename,omitempty"`
}

type ConsoleMessagesResult struct {
	Total    int              `json:"total"`
	Errors   int              `json:"errors"`
	Warnings int              `json:"warnings"`
	Messages []ConsoleMessage `json:"messages"`
	Text     string           `json:"text"`
}

type Dialog struct {
	Type          string `json:"type"`
	Message       string `json:"message"`
	DefaultPrompt string `json:"defaultPrompt,omitempty"`
	URL           string `json:"url,omitempty"`
}

type HandleDialogParams struct {
	Accept     bool   `json:"accept"`
	PromptText string `json:"promptText,omitempty"`
}

type EvaluateParams struct {
	Element  string `json:"element,omitempty"`
	Target   string `json:"target,omitempty"`
	Function string `json:"function"`
	Filename string `json:"filename,omitempty"`
}

type FileUploadParams struct {
	Paths []string `json:"paths,omitempty"`
}

type DropParams struct {
	Element string            `json:"element,omitempty"`
	Target  string            `json:"target"`
	Paths   []string          `json:"paths,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

type FindParams struct {
	Text  string `json:"text,omitempty"`
	Regex string `json:"regex,omitempty"`
}

type FormField struct {
	Element string `json:"element,omitempty"`
	Target  string `json:"target"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Value   string `json:"value"`
}

type FillFormParams struct {
	Fields []FormField `json:"fields"`
}

type PressKeyParams struct {
	Key string `json:"key"`
}

type TypeParams struct {
	Element string `json:"element,omitempty"`
	Target  string `json:"target"`
	Text    string `json:"text"`
	Submit  bool   `json:"submit,omitempty"`
	Slowly  bool   `json:"slowly,omitempty"`
}

type NavigateParams struct {
	URL string `json:"url"`
}

type NetworkRequestsParams struct {
	Static   bool   `json:"static"`
	Filter   string `json:"filter,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type NetworkRequestParams struct {
	Index    int    `json:"index"`
	Part     string `json:"part,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type NetworkRequestResult struct {
	Text string `json:"text,omitempty"`
	Data []byte `json:"data,omitempty"`
}

type NetworkResponse struct {
	Status     int64             `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	MimeType   string            `json:"mimeType"`
}

type NetworkRequest struct {
	ID             network.RequestID `json:"-"`
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	ResourceType   string            `json:"resourceType"`
	RequestHeaders map[string]string `json:"requestHeaders"`
	HasPostData    bool              `json:"hasPostData"`
	Response       *NetworkResponse  `json:"response,omitempty"`
	Failure        string            `json:"failure,omitempty"`
	StartedAt      time.Time         `json:"startedAt"`
	Duration       time.Duration     `json:"duration"`
	Finished       bool              `json:"finished"`
}

type TakeScreenshotParams struct {
	Element  string `json:"element,omitempty"`
	Target   string `json:"target,omitempty"`
	Type     string `json:"type,omitempty"`
	Filename string `json:"filename,omitempty"`
	FullPage bool   `json:"fullPage,omitempty"`
	Scale    string `json:"scale,omitempty"`
}

type SnapshotParams struct {
	Target   string `json:"target,omitempty"`
	Filename string `json:"filename,omitempty"`
	Depth    int    `json:"depth,omitempty"`
	Boxes    bool   `json:"boxes,omitempty"`
}

type ClickParams struct {
	Element     string   `json:"element,omitempty"`
	Target      string   `json:"target"`
	DoubleClick bool     `json:"doubleClick,omitempty"`
	Button      string   `json:"button,omitempty"`
	Modifiers   []string `json:"modifiers,omitempty"`
}

type DragParams struct {
	StartElement string `json:"startElement,omitempty"`
	StartTarget  string `json:"startTarget"`
	EndElement   string `json:"endElement,omitempty"`
	EndTarget    string `json:"endTarget"`
}

type HoverParams struct {
	Element string `json:"element,omitempty"`
	Target  string `json:"target"`
}

type SelectOptionParams struct {
	Element string   `json:"element,omitempty"`
	Target  string   `json:"target"`
	Values  []string `json:"values"`
}

type TabsParams struct {
	Action string `json:"action"`
	Index  *int   `json:"index,omitempty"`
	URL    string `json:"url,omitempty"`
}

type TabInfo struct {
	Index   int    `json:"index"`
	Current bool   `json:"current"`
	Title   string `json:"title"`
	URL     string `json:"url"`
}

type WaitForParams struct {
	Time     float64 `json:"time,omitempty"`
	Text     string  `json:"text,omitempty"`
	TextGone string  `json:"textGone,omitempty"`
}

type RunCodeFunc func(context.Context, *Page) (any, error)

func (b *Browser) currentPage() (*Page, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, ErrBrowserClosed
	}
	if b.lost {
		return nil, ErrSessionLost
	}
	if b.current == nil {
		return nil, errors.New("no browser tab is open")
	}
	return b.current, nil
}

func (b *Browser) BrowserClose(_ context.Context) error { return b.Close() }

func (b *Browser) BrowserResize(ctx context.Context, params ResizeParams) error {
	if params.Width <= 0 || params.Height <= 0 {
		return errors.New("width and height must be positive")
	}
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	return p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetDeviceMetricsOverride(params.Width, params.Height, 1, false).Do(ctx)
	}))
}

func (b *Browser) BrowserNavigate(ctx context.Context, params NavigateParams) error {
	if params.URL == "" {
		return errors.New("url is required")
	}
	p, err := b.currentPage()
	if err != nil {
		if errors.Is(err, ErrBrowserClosed) || errors.Is(err, ErrSessionLost) {
			return err
		}
		_, err = b.NewPage(ctx, params.URL)
		return err
	}
	return p.Navigate(ctx, params.URL)
}

func (b *Browser) BrowserNavigateBack(ctx context.Context) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	return p.run(ctx, chromedp.NavigateBack())
}

func (b *Browser) BrowserRunCodeUnsafe(ctx context.Context, run RunCodeFunc) (any, error) {
	if run == nil {
		return nil, errors.New("run callback is required")
	}
	p, err := b.currentPage()
	if err != nil {
		return nil, err
	}
	return run(ctx, p)
}

func (b *Browser) BrowserWaitFor(ctx context.Context, params WaitForParams) error {
	if params.Time <= 0 && params.Text == "" && params.TextGone == "" {
		return errors.New("either time, text or textGone must be provided")
	}
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	if params.Time > 0 {
		d := time.Duration(params.Time * float64(time.Second))
		if d > 30*time.Second {
			d = 30 * time.Second
		}
		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	if params.TextGone != "" {
		if err := p.waitForPageText(ctx, params.TextGone, false); err != nil {
			return err
		}
	}
	if params.Text != "" {
		if err := p.waitForPageText(ctx, params.Text, true); err != nil {
			return err
		}
	}
	return nil
}

func (p *Page) waitForPageText(ctx context.Context, text string, visible bool) error {
	encoded, _ := json.Marshal(text)
	expression := fmt.Sprintf(`(() => document.body && document.body.innerText.includes(%s))()`, encoded)
	deadline := time.Now().Add(30 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	for {
		var found bool
		if err := p.run(ctx, chromedp.Evaluate(expression, &found)); err != nil {
			return err
		}
		if found == visible {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timed out waiting for text %q to be %s", text, map[bool]string{true: "visible", false: "hidden"}[visible])
		}
		delay := 100 * time.Millisecond
		if remaining < delay {
			delay = remaining
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (b *Browser) BrowserConsoleMessages(ctx context.Context, params ConsoleMessagesParams) (ConsoleMessagesResult, error) {
	p, err := b.currentPage()
	if err != nil {
		return ConsoleMessagesResult{}, err
	}
	if err := p.run(ctx); err != nil {
		return ConsoleMessagesResult{}, err
	}
	level := params.Level
	if level == "" {
		level = ConsoleInfo
	}
	threshold, ok := map[ConsoleLevel]int{ConsoleError: 0, ConsoleWarning: 1, ConsoleInfo: 2, ConsoleDebug: 3}[level]
	if !ok {
		return ConsoleMessagesResult{}, fmt.Errorf("invalid console level %q", level)
	}
	p.state.mu.Lock()
	all := append([]ConsoleMessage(nil), p.state.console...)
	navigation := p.state.navigation
	p.state.mu.Unlock()
	result := ConsoleMessagesResult{Total: len(all)}
	for _, message := range all {
		if message.Level == ConsoleError {
			result.Errors++
		}
		if message.Level == ConsoleWarning {
			result.Warnings++
		}
		if !params.All && message.Navigation != navigation {
			continue
		}
		if map[ConsoleLevel]int{ConsoleError: 0, ConsoleWarning: 1, ConsoleInfo: 2, ConsoleDebug: 3}[message.Level] <= threshold {
			result.Messages = append(result.Messages, message)
		}
	}
	lines := []string{fmt.Sprintf("Total messages: %d (Errors: %d, Warnings: %d)", result.Total, result.Errors, result.Warnings)}
	if len(result.Messages) != result.Total {
		lines = append(lines, fmt.Sprintf("Returning %d messages for level %q", len(result.Messages), level))
	}
	lines = append(lines, "")
	for _, message := range result.Messages {
		lines = append(lines, fmt.Sprintf("[%s] %s", message.Level, message.Text))
	}
	result.Text = strings.Join(lines, "\n")
	if err := writeOptionalFile(params.Filename, []byte(result.Text)); err != nil {
		return ConsoleMessagesResult{}, err
	}
	return result, nil
}

func (b *Browser) BrowserHandleDialog(ctx context.Context, params HandleDialogParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	p.state.mu.Lock()
	dialog := p.state.dialog
	p.state.mu.Unlock()
	if dialog == nil {
		return errors.New("no dialog visible")
	}
	action := cdppage.HandleJavaScriptDialog(params.Accept)
	if params.PromptText != "" {
		action = action.WithPromptText(params.PromptText)
	}
	if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error { return action.Do(ctx) })); err != nil {
		return err
	}
	p.state.mu.Lock()
	p.state.dialog = nil
	p.state.mu.Unlock()
	return nil
}

func (b *Browser) BrowserEvaluate(ctx context.Context, params EvaluateParams) (any, error) {
	if strings.TrimSpace(params.Function) == "" {
		return nil, errors.New("function is required")
	}
	p, err := b.currentPage()
	if err != nil {
		return nil, err
	}
	var selector string
	if params.Target != "" {
		selector, err = p.targetSelector(ctx, params.Target)
		if err != nil {
			return nil, err
		}
	}
	functionJSON, _ := json.Marshal(params.Function)
	var expression string
	if selector == "" {
		expression = fmt.Sprintf(`(async () => { const value = eval("(" + %s + ")"); return typeof value === "function" ? await value() : await value; })()`, functionJSON)
	} else {
		selectorJSON, _ := json.Marshal(selector)
		expression = fmt.Sprintf(`(async () => { const value = eval("(" + %s + ")"); const element = document.querySelector(%s); if (!element) throw new Error("Target element not found"); return typeof value === "function" ? await value(element) : await value; })()`, functionJSON, selectorJSON)
	}
	var result any
	if err := p.run(ctx, chromedp.Evaluate(expression, &result, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
		return params.WithAwaitPromise(true)
	})); err != nil {
		return nil, err
	}
	if params.Filename != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeOptionalFile(params.Filename, data); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (b *Browser) BrowserFileUpload(ctx context.Context, params FileUploadParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	paths, err := validatePaths(params.Paths)
	if err != nil {
		return err
	}
	p.state.mu.Lock()
	backendID := p.state.fileChooser
	p.state.mu.Unlock()
	if backendID == 0 {
		return errors.New("no file chooser visible")
	}
	if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return dom.SetFileInputFiles(paths).WithBackendNodeID(backendID).Do(ctx)
	})); err != nil {
		return err
	}
	p.state.mu.Lock()
	p.state.fileChooser = 0
	p.state.mu.Unlock()
	return nil
}

func (b *Browser) BrowserDrop(ctx context.Context, params DropParams) error {
	if len(params.Paths) == 0 && len(params.Data) == 0 {
		return errors.New(`at least one of "paths" or "data" must be provided`)
	}
	paths, err := validatePaths(params.Paths)
	if err != nil {
		return err
	}
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	backendID, err := p.targetBackendID(ctx, params.Target)
	if err != nil {
		return err
	}
	var x, y float64
	if err := p.run(ctx, elementCenterAction(backendID, &x, &y)); err != nil {
		return err
	}
	items := make([]*input.DragDataItem, 0, len(params.Data))
	keys := make([]string, 0, len(params.Data))
	for mime := range params.Data {
		keys = append(keys, mime)
	}
	sort.Strings(keys)
	for _, mime := range keys {
		items = append(items, &input.DragDataItem{MimeType: mime, Data: params.Data[mime]})
	}
	data := &input.DragData{Items: items, Files: paths, DragOperationsMask: 1}
	return p.run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error { return input.DispatchDragEvent(input.DragEnter, x, y, data).Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error { return input.DispatchDragEvent(input.DragOver, x, y, data).Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error { return input.DispatchDragEvent(input.Drop, x, y, data).Do(ctx) }),
	)
}

func validatePaths(paths []string) ([]string, error) {
	result := make([]string, len(paths))
	for i, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("file path must be absolute: %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("access upload file %q: %w", path, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("upload path is a directory: %s", path)
		}
		result[i] = path
	}
	return result, nil
}

func writeOptionalFile(filename string, data []byte) error {
	if filename == "" {
		return nil
	}
	if dir := filepath.Dir(filename); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}
	if err := os.WriteFile(filename, data, 0o644); err != nil {
		return fmt.Errorf("write %q: %w", filename, err)
	}
	return nil
}

func (p *Page) targetSelector(ctx context.Context, target string) (string, error) {
	ref := normalizeRef(target)
	if ref == "" {
		return target, nil
	}
	p.state.mu.Lock()
	backendID, ok := p.state.refs[ref]
	p.state.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("snapshot reference %q not found; capture a new browser_snapshot", target)
	}
	if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		object, err := dom.ResolveNode().WithBackendNodeID(backendID).Do(ctx)
		if err != nil {
			return err
		}
		if object == nil || object.ObjectID == "" {
			return errors.New("snapshot target is no longer attached")
		}
		_, exception, err := runtime.CallFunctionOn(fmt.Sprintf(`function(){ this.setAttribute("data-pecdp-ref", %q); }`, ref)).WithObjectID(object.ObjectID).Do(ctx)
		if err != nil {
			return err
		}
		if exception != nil {
			return errors.New(exception.Text)
		}
		return nil
	})); err != nil {
		return "", err
	}
	return fmt.Sprintf(`[data-pecdp-ref=%q]`, ref), nil
}

func normalizeRef(target string) string {
	target = strings.TrimSpace(target)
	if strings.HasPrefix(target, "ref=") {
		target = strings.TrimPrefix(target, "ref=")
	}
	if matched, _ := regexp.MatchString(`^e[0-9]+$`, target); matched {
		return target
	}
	return ""
}

func (p *Page) targetBackendID(ctx context.Context, target string) (cdp.BackendNodeID, error) {
	if ref := normalizeRef(target); ref != "" {
		p.state.mu.Lock()
		id, ok := p.state.refs[ref]
		p.state.mu.Unlock()
		if !ok {
			return 0, fmt.Errorf("snapshot reference %q not found; capture a new browser_snapshot", target)
		}
		return id, nil
	}
	var nodes []*cdp.Node
	if err := p.run(ctx, chromedp.Nodes(target, &nodes, chromedp.ByQueryAll)); err != nil {
		return 0, err
	}
	if len(nodes) != 1 {
		return 0, fmt.Errorf("target %q matched %d elements; expected exactly one", target, len(nodes))
	}
	return nodes[0].BackendNodeID, nil
}

func elementCenterAction(backendID cdp.BackendNodeID, x, y *float64) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := dom.ScrollIntoViewIfNeeded().WithBackendNodeID(backendID).Do(ctx); err != nil {
			return err
		}
		model, err := dom.GetBoxModel().WithBackendNodeID(backendID).Do(ctx)
		if err != nil {
			return err
		}
		if model == nil || len(model.Border) != 8 {
			return errors.New("target element has no layout box")
		}
		*x = (model.Border[0] + model.Border[2] + model.Border[4] + model.Border[6]) / 4
		*y = (model.Border[1] + model.Border[3] + model.Border[5] + model.Border[7]) / 4
		return nil
	})
}

func (b *Browser) BrowserFillForm(ctx context.Context, params FillFormParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	for _, field := range params.Fields {
		selector, err := p.targetSelector(ctx, field.Target)
		if err != nil {
			return fmt.Errorf("fill %q: %w", field.Name, err)
		}
		switch field.Type {
		case "textbox", "slider":
			if err := p.fillValue(ctx, selector, field.Value); err != nil {
				return fmt.Errorf("fill %q: %w", field.Name, err)
			}
		case "checkbox", "radio":
			checked, err := strconv.ParseBool(field.Value)
			if err != nil {
				return fmt.Errorf("fill %q: checkbox/radio value must be true or false", field.Name)
			}
			if err := p.setChecked(ctx, selector, checked); err != nil {
				return fmt.Errorf("fill %q: %w", field.Name, err)
			}
		case "combobox":
			if err := p.selectValues(ctx, selector, []string{field.Value}, true); err != nil {
				return fmt.Errorf("fill %q: %w", field.Name, err)
			}
		default:
			return fmt.Errorf("fill %q: unsupported field type %q", field.Name, field.Type)
		}
	}
	return nil
}

func (b *Browser) BrowserPressKey(ctx context.Context, params PressKeyParams) error {
	if params.Key == "" {
		return errors.New("key is required")
	}
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	key, modifiers, err := encodeKey(params.Key)
	if err != nil {
		return err
	}
	return p.run(ctx, chromedp.KeyEvent(key, chromedp.KeyModifiers(modifiers...)))
}

func (b *Browser) BrowserType(ctx context.Context, params TypeParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	selector, err := p.targetSelector(ctx, params.Target)
	if err != nil {
		return err
	}
	if !params.Slowly {
		if err := p.fillValue(ctx, selector, params.Text); err != nil {
			return err
		}
		if params.Submit {
			return p.run(ctx, chromedp.SendKeys(selector, kb.Enter, chromedp.ByQuery))
		}
		return nil
	}
	actions := []chromedp.Action{chromedp.Focus(selector, chromedp.ByQuery)}
	for _, r := range params.Text {
		actions = append(actions, chromedp.KeyEvent(string(r)))
	}
	if params.Submit {
		actions = append(actions, chromedp.KeyEvent(kb.Enter))
	}
	return p.run(ctx, actions...)
}

func (b *Browser) BrowserClick(ctx context.Context, params ClickParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	backendID, err := p.targetBackendID(ctx, params.Target)
	if err != nil {
		return err
	}
	var x, y float64
	if err := p.run(ctx, elementCenterAction(backendID, &x, &y)); err != nil {
		return err
	}
	button, buttons, err := mouseButton(params.Button)
	if err != nil {
		return err
	}
	modifiers, err := inputModifiers(params.Modifiers)
	if err != nil {
		return err
	}
	count := int64(1)
	if params.DoubleClick {
		count = 2
	}
	actions := []chromedp.Action{
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, x, y).WithModifiers(modifiers).Do(ctx)
		}),
	}
	for i := int64(1); i <= count; i++ {
		clickCount := i
		actions = append(actions,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MousePressed, x, y).WithButton(button).WithButtons(buttons).WithModifiers(modifiers).WithClickCount(clickCount).Do(ctx)
			}),
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MouseReleased, x, y).WithButton(button).WithModifiers(modifiers).WithClickCount(clickCount).Do(ctx)
			}),
		)
	}
	return p.run(ctx, actions...)
}

func (b *Browser) BrowserDrag(ctx context.Context, params DragParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	startID, err := p.targetBackendID(ctx, params.StartTarget)
	if err != nil {
		return fmt.Errorf("resolve drag start: %w", err)
	}
	endID, err := p.targetBackendID(ctx, params.EndTarget)
	if err != nil {
		return fmt.Errorf("resolve drag end: %w", err)
	}
	var startX, startY, endX, endY float64
	if err := p.run(ctx, elementCenterAction(startID, &startX, &startY), elementCenterAction(endID, &endX, &endY)); err != nil {
		return err
	}
	return p.run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, startX, startY).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MousePressed, startX, startY).WithButton(input.Left).WithButtons(1).WithClickCount(1).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, (startX+endX)/2, (startY+endY)/2).WithButton(input.Left).WithButtons(1).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseMoved, endX, endY).WithButton(input.Left).WithButtons(1).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			return input.DispatchMouseEvent(input.MouseReleased, endX, endY).WithButton(input.Left).WithClickCount(1).Do(ctx)
		}),
	)
}

func (b *Browser) BrowserHover(ctx context.Context, params HoverParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	backendID, err := p.targetBackendID(ctx, params.Target)
	if err != nil {
		return err
	}
	var x, y float64
	if err := p.run(ctx, elementCenterAction(backendID, &x, &y)); err != nil {
		return err
	}
	return p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
	}))
}

func (b *Browser) BrowserSelectOption(ctx context.Context, params SelectOptionParams) error {
	p, err := b.currentPage()
	if err != nil {
		return err
	}
	selector, err := p.targetSelector(ctx, params.Target)
	if err != nil {
		return err
	}
	return p.selectValues(ctx, selector, params.Values, false)
}

func (p *Page) setChecked(ctx context.Context, selector string, checked bool) error {
	selectorJSON, _ := json.Marshal(selector)
	expression := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!el) throw new Error("Target element not found"); el.checked = %t; el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); })()`, selectorJSON, checked)
	return p.run(ctx, chromedp.Evaluate(expression, nil))
}

func (p *Page) fillValue(ctx context.Context, selector, value string) error {
	selectorJSON, _ := json.Marshal(selector)
	valueJSON, _ := json.Marshal(value)
	expression := fmt.Sprintf(`(() => {
  const el = document.querySelector(%s);
  if (!el) throw new Error("Target element not found");
  el.focus();
  if (el.isContentEditable) {
    el.textContent = %s;
  } else {
    const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
    const setter = Object.getOwnPropertyDescriptor(proto, "value").set;
    setter.call(el, %s);
  }
  el.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: %s }));
  el.dispatchEvent(new Event("change", { bubbles: true }));
})()`, selectorJSON, valueJSON, valueJSON, valueJSON)
	return p.run(ctx, chromedp.Evaluate(expression, nil))
}

func (p *Page) selectValues(ctx context.Context, selector string, values []string, allowLabel bool) error {
	selectorJSON, _ := json.Marshal(selector)
	valuesJSON, _ := json.Marshal(values)
	expression := fmt.Sprintf(`(() => { const el = document.querySelector(%s); if (!(el instanceof HTMLSelectElement)) throw new Error("Target is not a select element"); const values = %s; let count = 0; for (const option of el.options) { const selected = values.includes(option.value)%s; option.selected = selected; if (selected) count++; } if (count !== values.length) throw new Error("One or more options were not found"); el.dispatchEvent(new Event("input", {bubbles:true})); el.dispatchEvent(new Event("change", {bubbles:true})); })()`, selectorJSON, valuesJSON, map[bool]string{true: ` || values.includes(option.label)`, false: ""}[allowLabel])
	return p.run(ctx, chromedp.Evaluate(expression, nil))
}

func encodeKey(key string) (string, []input.Modifier, error) {
	parts := strings.Split(key, "+")
	modifiers := make([]input.Modifier, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		switch strings.ToLower(part) {
		case "alt":
			modifiers = append(modifiers, input.ModifierAlt)
		case "control", "ctrl":
			modifiers = append(modifiers, input.ModifierCtrl)
		case "meta", "command", "controlormeta":
			modifiers = append(modifiers, input.ModifierMeta)
		case "shift":
			modifiers = append(modifiers, input.ModifierShift)
		default:
			return "", nil, fmt.Errorf("unsupported key modifier %q", part)
		}
	}
	name := parts[len(parts)-1]
	keys := map[string]string{
		"Backspace": kb.Backspace, "Tab": kb.Tab, "Enter": kb.Enter, "Escape": kb.Escape,
		"Delete": kb.Delete, "ArrowDown": kb.ArrowDown, "ArrowLeft": kb.ArrowLeft,
		"ArrowRight": kb.ArrowRight, "ArrowUp": kb.ArrowUp, "End": kb.End, "Home": kb.Home,
		"PageDown": kb.PageDown, "PageUp": kb.PageUp, "Insert": kb.Insert,
		"F1": kb.F1, "F2": kb.F2, "F3": kb.F3, "F4": kb.F4, "F5": kb.F5, "F6": kb.F6,
		"F7": kb.F7, "F8": kb.F8, "F9": kb.F9, "F10": kb.F10, "F11": kb.F11, "F12": kb.F12,
	}
	if encoded, ok := keys[name]; ok {
		return encoded, modifiers, nil
	}
	if len([]rune(name)) == 1 {
		return name, modifiers, nil
	}
	return "", nil, fmt.Errorf("unsupported key %q", name)
}

func inputModifiers(names []string) (input.Modifier, error) {
	var result input.Modifier
	for _, name := range names {
		switch name {
		case "Alt":
			result |= input.ModifierAlt
		case "Control":
			result |= input.ModifierCtrl
		case "ControlOrMeta", "Meta":
			result |= input.ModifierMeta
		case "Shift":
			result |= input.ModifierShift
		default:
			return 0, fmt.Errorf("invalid modifier %q", name)
		}
	}
	return result, nil
}

func mouseButton(name string) (input.MouseButton, int64, error) {
	switch name {
	case "", "left":
		return input.Left, 1, nil
	case "right":
		return input.Right, 2, nil
	case "middle":
		return input.Middle, 4, nil
	default:
		return input.None, 0, fmt.Errorf("invalid mouse button %q", name)
	}
}

func (b *Browser) BrowserSnapshot(ctx context.Context, params SnapshotParams) (string, error) {
	p, err := b.currentPage()
	if err != nil {
		return "", err
	}
	var targetID cdp.BackendNodeID
	if params.Target != "" {
		targetID, err = p.targetBackendID(ctx, params.Target)
		if err != nil {
			return "", err
		}
	}
	var nodes []*accessibility.Node
	if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		nodes, err = accessibility.GetFullAXTree().Do(ctx)
		return err
	})); err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", errors.New("accessibility snapshot is empty")
	}
	byID := make(map[accessibility.NodeID]*accessibility.Node, len(nodes))
	for _, node := range nodes {
		byID[node.NodeID] = node
	}
	root := nodes[0]
	if targetID != 0 {
		root = nil
		for _, node := range nodes {
			if node.BackendDOMNodeID == targetID {
				root = node
				break
			}
		}
		if root == nil {
			return "", errors.New("target element is not represented in the accessibility tree")
		}
	}
	boxes := make(map[cdp.BackendNodeID]string)
	if params.Boxes {
		seen := make(map[cdp.BackendNodeID]bool)
		actions := make([]chromedp.Action, 0)
		var collect func(*accessibility.Node, int)
		collect = func(node *accessibility.Node, depth int) {
			if node == nil || (params.Depth > 0 && depth > params.Depth) {
				return
			}
			if node.BackendDOMNodeID != 0 && !seen[node.BackendDOMNodeID] {
				id := node.BackendDOMNodeID
				seen[id] = true
				actions = append(actions, chromedp.ActionFunc(func(ctx context.Context) error {
					model, err := dom.GetBoxModel().WithBackendNodeID(id).Do(ctx)
					if err != nil || model == nil || len(model.Border) != 8 {
						return nil
					}
					x := min4(model.Border[0], model.Border[2], model.Border[4], model.Border[6])
					y := min4(model.Border[1], model.Border[3], model.Border[5], model.Border[7])
					boxes[id] = fmt.Sprintf("[box=%s,%s,%d,%d]", formatFloat(x), formatFloat(y), model.Width, model.Height)
					return nil
				}))
			}
			for _, childID := range node.ChildIDs {
				collect(byID[childID], depth+1)
			}
		}
		collect(root, 0)
		if err := p.run(ctx, actions...); err != nil {
			return "", err
		}
	}

	refs := make(map[string]cdp.BackendNodeID)
	refCount := 0
	lines := make([]string, 0, len(nodes))
	var render func(*accessibility.Node, int, int)
	render = func(node *accessibility.Node, indent, depth int) {
		if node == nil || (params.Depth > 0 && depth > params.Depth) {
			return
		}
		if node.Ignored {
			for _, childID := range node.ChildIDs {
				render(byID[childID], indent, depth)
			}
			return
		}
		role := axValue(node.Role)
		name := axValue(node.Name)
		if role == "" {
			role = "generic"
		}
		line := strings.Repeat("  ", indent) + "- " + role
		if name != "" {
			line += " " + strconv.Quote(name)
		}
		if node.BackendDOMNodeID != 0 {
			refCount++
			ref := fmt.Sprintf("e%d", refCount)
			refs[ref] = node.BackendDOMNodeID
			line += " [ref=" + ref + "]"
		}
		if value := axValue(node.Value); value != "" && value != name {
			line += " " + strconv.Quote(value)
		}
		for _, property := range node.Properties {
			switch string(property.Name) {
			case "checked", "disabled", "expanded", "focused", "pressed", "readonly", "required", "selected":
				if value := axValue(property.Value); value != "" && value != "false" {
					line += " [" + string(property.Name)
					if value != "true" {
						line += "=" + value
					}
					line += "]"
				}
			}
		}
		if box := boxes[node.BackendDOMNodeID]; box != "" {
			line += " " + box
		}
		lines = append(lines, line)
		for _, childID := range node.ChildIDs {
			render(byID[childID], indent+1, depth+1)
		}
	}
	render(root, 0, 0)
	text := strings.Join(lines, "\n")
	p.state.mu.Lock()
	p.state.refs = refs
	p.state.mu.Unlock()
	if err := writeOptionalFile(params.Filename, []byte(text)); err != nil {
		return "", err
	}
	return text, nil
}

func (b *Browser) BrowserFind(ctx context.Context, params FindParams) (string, error) {
	if (params.Text == "") == (params.Regex == "") {
		return "", errors.New(`provide exactly one of "text" or "regex"`)
	}
	snapshot, err := b.BrowserSnapshot(ctx, SnapshotParams{})
	if err != nil {
		return "", err
	}
	var matches func(string) bool
	query := strconv.Quote(params.Text)
	if params.Text != "" {
		needle := strings.ToLower(params.Text)
		matches = func(line string) bool { return strings.Contains(strings.ToLower(line), needle) }
	} else {
		re, err := compileToolRegex(params.Regex)
		if err != nil {
			return "", err
		}
		query = params.Regex
		matches = re.MatchString
	}
	lines := strings.Split(snapshot, "\n")
	indices := make([]int, 0)
	for i, line := range lines {
		if matches(line) {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return fmt.Sprintf("No matches found for %s.", query), nil
	}
	windows := make([][2]int, 0)
	for _, index := range indices {
		start, end := max(0, index-3), min(len(lines)-1, index+3)
		if len(windows) != 0 && start <= windows[len(windows)-1][1]+1 {
			windows[len(windows)-1][1] = max(windows[len(windows)-1][1], end)
		} else {
			windows = append(windows, [2]int{start, end})
		}
	}
	parts := make([]string, 0, len(windows))
	for _, window := range windows {
		parts = append(parts, strings.Join(lines[window[0]:window[1]+1], "\n"))
	}
	word := "matches"
	if len(indices) == 1 {
		word = "match"
	}
	return fmt.Sprintf("Found %d %s for %s:\n\n%s", len(indices), word, query, strings.Join(parts, "\n\n----\n\n")), nil
}

func axValue(value *accessibility.Value) string {
	if value == nil || len(value.Value) == 0 {
		return ""
	}
	var decoded any
	if json.Unmarshal(value.Value, &decoded) != nil {
		return ""
	}
	switch value := decoded.(type) {
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		data, _ := json.Marshal(value)
		return string(data)
	}
}

func compileToolRegex(source string) (*regexp.Regexp, error) {
	pattern, flags := source, ""
	if strings.HasPrefix(source, "/") {
		if index := strings.LastIndex(source[1:], "/"); index >= 0 {
			index++
			pattern, flags = source[1:index], source[index+1:]
		}
	}
	prefix := ""
	for _, flag := range flags {
		switch flag {
		case 'i', 'm', 's':
			prefix += string(flag)
		case 'g':
		default:
			return nil, fmt.Errorf("invalid regular expression flag %q", flag)
		}
	}
	if prefix != "" {
		pattern = "(?" + prefix + ")" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regular expression: %w", err)
	}
	return re, nil
}

func min4(a, b, c, d float64) float64 { return min(min(a, b), min(c, d)) }

func formatFloat(value float64) string { return strconv.FormatFloat(value, 'f', -1, 64) }

func (b *Browser) BrowserNetworkRequests(ctx context.Context, params NetworkRequestsParams) (string, error) {
	p, err := b.currentPage()
	if err != nil {
		return "", err
	}
	if err := p.run(ctx); err != nil {
		return "", err
	}
	var filter *regexp.Regexp
	if params.Filter != "" {
		filter, err = compileToolRegex(params.Filter)
		if err != nil {
			return "", err
		}
	}
	requests := p.networkRequests()
	lines := make([]string, 0, len(requests))
	hidden := 0
	for i, request := range requests {
		if !params.Static && !isFetchRequest(request) && request.Response != nil && request.Response.Status < 400 && request.Failure == "" {
			hidden++
			continue
		}
		if filter != nil && !filter.MatchString(request.URL) {
			continue
		}
		line := fmt.Sprintf("%d. [%s] %s", i+1, strings.ToUpper(request.Method), request.URL)
		if request.Response != nil {
			line += fmt.Sprintf(" => [%d] %s", request.Response.Status, request.Response.StatusText)
		} else if request.Failure != "" {
			line += " => [FAILED] " + request.Failure
		}
		lines = append(lines, line)
	}
	if hidden != 0 {
		lines = append(lines, "", fmt.Sprintf("Note: %d successful static requests not shown; set static=true to include them.", hidden))
	}
	text := strings.Join(lines, "\n")
	if err := writeOptionalFile(params.Filename, []byte(text)); err != nil {
		return "", err
	}
	return text, nil
}

func (b *Browser) BrowserNetworkRequest(ctx context.Context, params NetworkRequestParams) (NetworkRequestResult, error) {
	if params.Index < 1 {
		return NetworkRequestResult{}, errors.New("network request index must be at least 1")
	}
	p, err := b.currentPage()
	if err != nil {
		return NetworkRequestResult{}, err
	}
	requests := p.networkRequests()
	if params.Index > len(requests) {
		return NetworkRequestResult{}, fmt.Errorf("request #%d not found; use browser_network_requests to see available indexes", params.Index)
	}
	request := requests[params.Index-1]
	result := NetworkRequestResult{}
	switch params.Part {
	case "":
		result.Text = renderNetworkRequest(params.Index, request)
	case "request-headers":
		result.Text = renderHeaders(request.RequestHeaders)
	case "response-headers":
		if request.Response != nil {
			result.Text = renderHeaders(request.Response.Headers)
		}
	case "request-body":
		if request.HasPostData {
			var data []byte
			if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				data, err = network.GetRequestPostData(request.ID).Do(ctx)
				return err
			})); err != nil {
				return NetworkRequestResult{}, err
			}
			result.Data, result.Text = data, string(data)
		}
	case "response-body":
		if request.Response != nil {
			var data []byte
			if err := p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				data, err = network.GetResponseBody(request.ID).Do(ctx)
				return err
			})); err != nil {
				return NetworkRequestResult{}, err
			}
			result.Data = data
			if isTextMime(request.Response.MimeType) {
				result.Text = string(data)
			}
		}
	default:
		return NetworkRequestResult{}, fmt.Errorf("invalid network request part %q", params.Part)
	}
	data := result.Data
	if data == nil {
		data = []byte(result.Text)
	}
	if err := writeOptionalFile(params.Filename, data); err != nil {
		return NetworkRequestResult{}, err
	}
	return result, nil
}

func (p *Page) networkRequests() []*NetworkRequest {
	p.state.mu.Lock()
	defer p.state.mu.Unlock()
	result := make([]*NetworkRequest, 0, len(p.state.requests))
	for _, request := range p.state.requests {
		copy := *request
		copy.RequestHeaders = cloneStringMap(request.RequestHeaders)
		if request.Response != nil {
			response := *request.Response
			response.Headers = cloneStringMap(request.Response.Headers)
			copy.Response = &response
		}
		result = append(result, &copy)
	}
	return result
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func isFetchRequest(request *NetworkRequest) bool {
	return strings.EqualFold(request.ResourceType, "XHR") || strings.EqualFold(request.ResourceType, "Fetch")
}

func renderNetworkRequest(index int, request *NetworkRequest) string {
	lines := []string{fmt.Sprintf("#%d [%s] %s", index, strings.ToUpper(request.Method), request.URL), "", "  General"}
	if request.Response != nil {
		lines = append(lines, fmt.Sprintf("    status:    [%d] %s", request.Response.Status, request.Response.StatusText))
	} else if request.Failure != "" {
		lines = append(lines, "    status:    [FAILED] "+request.Failure)
	}
	if request.Duration > 0 {
		lines = append(lines, fmt.Sprintf("    duration:  %dms", request.Duration.Milliseconds()))
	}
	lines = append(lines, "    type:      "+request.ResourceType)
	if request.Response != nil && request.Response.MimeType != "" {
		lines = append(lines, "    mimeType:  "+request.Response.MimeType)
	}
	if headers := renderHeaders(request.RequestHeaders); headers != "" {
		lines = append(lines, "", "  Request headers")
		for _, line := range strings.Split(headers, "\n") {
			lines = append(lines, "    "+line)
		}
	}
	if request.Response != nil {
		if headers := renderHeaders(request.Response.Headers); headers != "" {
			lines = append(lines, "", "  Response headers")
			for _, line := range strings.Split(headers, "\n") {
				lines = append(lines, "    "+line)
			}
		}
	}
	if request.HasPostData {
		lines = append(lines, "", fmt.Sprintf(`Call browser_network_request with index=%d and part="request-body" to read the request body.`, index))
	}
	if request.Response != nil && request.Response.Status != 204 && request.Response.Status != 304 && (request.Response.Status < 100 || request.Response.Status >= 200) {
		lines = append(lines, fmt.Sprintf(`Call browser_network_request with index=%d and part="response-body" to read the response body.`, index))
	}
	return strings.Join(lines, "\n")
}

func renderHeaders(headers map[string]string) string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+": "+headers[key])
	}
	return strings.Join(lines, "\n")
}

func isTextMime(mime string) bool {
	mime = strings.ToLower(mime)
	return strings.HasPrefix(mime, "text/") || strings.Contains(mime, "json") || strings.Contains(mime, "javascript") || strings.Contains(mime, "xml") || strings.Contains(mime, "svg") || strings.Contains(mime, "x-www-form-urlencoded")
}

func (b *Browser) BrowserTakeScreenshot(ctx context.Context, params TakeScreenshotParams) ([]byte, error) {
	if params.FullPage && params.Target != "" {
		return nil, errors.New("fullPage cannot be used with element screenshots")
	}
	if params.Scale != "" && params.Scale != "css" && params.Scale != "device" {
		return nil, fmt.Errorf("invalid screenshot scale %q", params.Scale)
	}
	format := params.Type
	if format == "" && params.Filename != "" {
		switch strings.ToLower(filepath.Ext(params.Filename)) {
		case ".jpg", ".jpeg":
			format = "jpeg"
		case ".webp":
			format = "webp"
		case ".png":
			format = "png"
		}
	}
	if format == "" {
		format = "png"
	}
	var cdpFormat cdppage.CaptureScreenshotFormat
	switch format {
	case "png":
		cdpFormat = cdppage.CaptureScreenshotFormatPng
	case "jpeg":
		cdpFormat = cdppage.CaptureScreenshotFormatJpeg
	case "webp":
		cdpFormat = cdppage.CaptureScreenshotFormatWebp
	default:
		return nil, fmt.Errorf("invalid screenshot type %q", format)
	}
	p, err := b.currentPage()
	if err != nil {
		return nil, err
	}
	var backendID cdp.BackendNodeID
	if params.Target != "" {
		backendID, err = p.targetBackendID(ctx, params.Target)
		if err != nil {
			return nil, err
		}
	}
	var data []byte
	err = p.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		capture := cdppage.CaptureScreenshot().WithFormat(cdpFormat).WithCaptureBeyondViewport(params.FullPage || backendID != 0)
		if cdpFormat == cdppage.CaptureScreenshotFormatJpeg {
			capture = capture.WithQuality(90)
		}
		if backendID != 0 {
			if err := dom.ScrollIntoViewIfNeeded().WithBackendNodeID(backendID).Do(ctx); err != nil {
				return err
			}
			model, err := dom.GetBoxModel().WithBackendNodeID(backendID).Do(ctx)
			if err != nil {
				return err
			}
			if model == nil || len(model.Border) != 8 {
				return errors.New("target element has no layout box")
			}
			x := min4(model.Border[0], model.Border[2], model.Border[4], model.Border[6])
			y := min4(model.Border[1], model.Border[3], model.Border[5], model.Border[7])
			capture = capture.WithClip(&cdppage.Viewport{X: x, Y: y, Width: float64(model.Width), Height: float64(model.Height), Scale: 1})
		} else if params.FullPage {
			_, _, content, _, _, cssContent, err := cdppage.GetLayoutMetrics().Do(ctx)
			if err != nil {
				return err
			}
			if cssContent != nil {
				content = cssContent
			}
			if content == nil {
				return errors.New("page content size is unavailable")
			}
			capture = capture.WithClip(&cdppage.Viewport{X: content.X, Y: content.Y, Width: content.Width, Height: content.Height, Scale: 1})
		}
		data, err = capture.Do(ctx)
		return err
	}))
	if err != nil {
		return nil, err
	}
	if err := writeOptionalFile(params.Filename, data); err != nil {
		return nil, err
	}
	return data, nil
}

func (b *Browser) BrowserTabs(ctx context.Context, params TabsParams) ([]TabInfo, error) {
	switch params.Action {
	case "list":
	case "new":
		if _, err := b.NewPage(ctx, params.URL); err != nil {
			return nil, err
		}
	case "close":
		var target *Page
		b.mu.Lock()
		if params.Index == nil {
			target = b.current
		} else if *params.Index >= 0 && *params.Index < len(b.pageOrder) {
			target = b.pageOrder[*params.Index]
		}
		b.mu.Unlock()
		if target == nil {
			return nil, errors.New("tab index is out of range or no current tab exists")
		}
		if err := target.Close(); err != nil {
			return nil, err
		}
	case "select":
		if params.Index == nil {
			return nil, errors.New("tab index is required")
		}
		b.mu.Lock()
		if *params.Index < 0 || *params.Index >= len(b.pageOrder) {
			b.mu.Unlock()
			return nil, errors.New("tab index is out of range")
		}
		target := b.pageOrder[*params.Index]
		b.current = target
		b.mu.Unlock()
		if err := target.run(ctx, chromedp.ActionFunc(func(ctx context.Context) error { return cdppage.BringToFront().Do(ctx) })); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("invalid tabs action %q", params.Action)
	}
	return b.tabInfo(ctx)
}

func (b *Browser) tabInfo(ctx context.Context) ([]TabInfo, error) {
	b.mu.Lock()
	pages := append([]*Page(nil), b.pageOrder...)
	current := b.current
	b.mu.Unlock()
	result := make([]TabInfo, 0, len(pages))
	for index, page := range pages {
		var title, url string
		if err := page.run(ctx, chromedp.Title(&title), chromedp.Location(&url)); err != nil {
			return nil, err
		}
		result = append(result, TabInfo{Index: index, Current: page == current, Title: title, URL: url})
	}
	return result, nil
}
