package pebridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/chromedp"

	browserlauncher "github.com/jx3fans/playwright-extension-bridge-go/internal/browser"
	"github.com/jx3fans/playwright-extension-bridge-go/internal/protocol"
	"github.com/jx3fans/playwright-extension-bridge-go/internal/relay"
)

var (
	ErrBrowserClosed = errors.New("bridge browser is closed")
	ErrPageClosed    = errors.New("bridge page is closed")
	ErrSessionLost   = errors.New("bridge session is lost")
)

type Options struct {
	ExtensionToken string
	BrowserPath    string
	ClientName     string
	ConnectTimeout time.Duration
}

// Browser owns one extension relay, one chromedp browser connection, and an
// internal anchor target that keeps the remote allocator alive.
type Browser struct {
	mu sync.Mutex

	server  *relay.Server
	handler *protocol.Handler

	allocatorCancel context.CancelFunc
	rootContext     context.Context
	rootCancel      context.CancelFunc

	pages     map[*Page]struct{}
	pageOrder []*Page
	current   *Page
	closed    bool
	lost      bool
}

// Page is a chromedp target context created under Browser's anchor context.
type Page struct {
	mu sync.Mutex

	browser *Browser
	ctx     context.Context
	cancel  context.CancelFunc
	state   *pageState
	closed  bool
}

type ScreenshotOptions struct {
	FullPage bool
	Quality  int
}

func New(ctx context.Context, options Options) (*Browser, error) {
	if ctx == nil {
		return nil, errors.New("nil context")
	}
	timeout := options.ConnectTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	handler := protocol.NewHandler()
	server, err := relay.NewServer(handler)
	if err != nil {
		return nil, fmt.Errorf("create relay: %w", err)
	}
	server.Start()
	cleanupServer := true
	defer func() {
		if cleanupServer {
			closeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Close(closeCtx)
		}
	}()

	clientName := options.ClientName
	if clientName == "" {
		clientName = "亚马逊选品专家"
	}
	if err := browserlauncher.Launch(browserlauncher.LaunchOptions{
		BrowserPath:    options.BrowserPath,
		ExtensionToken: options.ExtensionToken,
		RelayEndpoint:  server.ExtensionEndpoint(),
		ClientName:     clientName,
	}); err != nil {
		return nil, err
	}

	waitCtx, cancelWait := context.WithTimeout(ctx, timeout)
	err = handler.WaitReady(waitCtx)
	cancelWait()
	if err != nil {
		return nil, fmt.Errorf("wait for Playwright extension: %w (check the extension token and active Edge profile)", err)
	}

	allocatorCtx, allocatorCancel := chromedp.NewRemoteAllocator(ctx, server.CDPEndpoint(), chromedp.NoModifyURL)
	rootCtx, rootCancel := chromedp.NewContext(allocatorCtx)
	if err := runWithCancellation(ctx, func() error { return chromedp.Run(rootCtx) }, rootCancel); err != nil {
		allocatorCancel()
		return nil, fmt.Errorf("initialize chromedp bridge: %w", err)
	}

	b := &Browser{
		server:          server,
		handler:         handler,
		allocatorCancel: allocatorCancel,
		rootContext:     rootCtx,
		rootCancel:      rootCancel,
		pages:           make(map[*Page]struct{}),
	}
	cleanupServer = false
	go b.watchDisconnect()
	return b, nil
}

func (b *Browser) NewPage(ctx context.Context, url string) (*Page, error) {
	if err := b.stateError(); err != nil {
		return nil, err
	}
	pageCtx, cancel := chromedp.NewContext(b.rootContext)
	page := &Page{browser: b, ctx: pageCtx, cancel: cancel, state: newPageState()}
	page.initObservers()
	actions := page.observerActions()
	if url != "" {
		actions = append(actions, chromedp.Navigate(url))
	}
	if err := runWithCancellation(ctx, func() error { return chromedp.Run(pageCtx, actions...) }, cancel); err != nil {
		cancel()
		return nil, fmt.Errorf("create page: %w", err)
	}
	b.mu.Lock()
	if b.closed || b.lost {
		b.mu.Unlock()
		cancel()
		return nil, b.stateError()
	}
	b.pages[page] = struct{}{}
	b.pageOrder = append(b.pageOrder, page)
	b.current = page
	b.mu.Unlock()
	return page, nil
}

func (b *Browser) Pages() []*Page {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := append([]*Page(nil), b.pageOrder...)
	return result
}

func (b *Browser) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	pages := make([]*Page, 0, len(b.pages))
	for page := range b.pages {
		pages = append(pages, page)
	}
	b.mu.Unlock()

	for _, page := range pages {
		_ = page.Close()
	}
	b.rootCancel()
	b.allocatorCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return b.server.Close(ctx)
}

func (b *Browser) stateError() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return ErrBrowserClosed
	}
	if b.lost {
		return ErrSessionLost
	}
	return nil
}

func (b *Browser) watchDisconnect() {
	<-b.handler.Done()
	b.mu.Lock()
	if !b.closed {
		b.lost = true
	}
	b.mu.Unlock()
}

func (b *Browser) removePage(page *Page) {
	b.mu.Lock()
	delete(b.pages, page)
	for i, candidate := range b.pageOrder {
		if candidate == page {
			b.pageOrder = append(b.pageOrder[:i], b.pageOrder[i+1:]...)
			break
		}
	}
	if b.current == page {
		b.current = nil
		if len(b.pageOrder) != 0 {
			b.current = b.pageOrder[len(b.pageOrder)-1]
		}
	}
	b.mu.Unlock()
}

func (p *Page) Navigate(ctx context.Context, url string) error {
	return p.run(ctx, chromedp.Navigate(url))
}

func (p *Page) Click(ctx context.Context, selector string) error {
	return p.run(ctx, chromedp.Click(selector, chromedp.ByQuery))
}

func (p *Page) Fill(ctx context.Context, selector, value string) error {
	return p.run(ctx,
		chromedp.Clear(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, value, chromedp.ByQuery),
	)
}

func (p *Page) Text(ctx context.Context, selector string) (string, error) {
	var value string
	err := p.run(ctx, chromedp.Text(selector, &value, chromedp.ByQuery))
	return value, err
}

func (p *Page) Title(ctx context.Context) (string, error) {
	var value string
	err := p.run(ctx, chromedp.Title(&value))
	return value, err
}

func (p *Page) Evaluate(ctx context.Context, expression string, result any) error {
	return p.run(ctx, chromedp.Evaluate(expression, result))
}

func (p *Page) Screenshot(ctx context.Context, options ScreenshotOptions) ([]byte, error) {
	var data []byte
	if options.FullPage {
		quality := options.Quality
		if quality == 0 {
			quality = 100
		}
		if quality < 0 || quality > 100 {
			return nil, errors.New("screenshot quality must be between 0 and 100")
		}
		if err := p.run(ctx, chromedp.FullScreenshot(&data, quality)); err != nil {
			return nil, err
		}
		return data, nil
	}
	if err := p.run(ctx, chromedp.CaptureScreenshot(&data)); err != nil {
		return nil, err
	}
	return data, nil
}

func (p *Page) Context() context.Context { return p.ctx }

func (p *Page) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	cancel := p.cancel
	p.mu.Unlock()
	cancel()
	p.browser.removePage(p)
	return nil
}

func (p *Page) run(ctx context.Context, actions ...chromedp.Action) error {
	p.mu.Lock()
	closed := p.closed
	pageCtx := p.ctx
	p.mu.Unlock()
	if closed {
		return ErrPageClosed
	}
	if err := p.browser.stateError(); err != nil {
		return err
	}
	if ctx == nil {
		return errors.New("nil context")
	}
	runCtx, cancel := mergeContext(pageCtx, ctx)
	defer cancel()
	if err := chromedp.Run(runCtx, actions...); err != nil {
		if stateErr := p.browser.stateError(); stateErr != nil {
			return stateErr
		}
		return err
	}
	return nil
}

func mergeContext(base, operation context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(base)
	stop := context.AfterFunc(operation, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func runWithCancellation(ctx context.Context, run func() error, stop func()) error {
	done := make(chan error, 1)
	go func() { done <- run() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		stop()
		<-done
		return ctx.Err()
	}
}
