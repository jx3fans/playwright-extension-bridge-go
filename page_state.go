package pebridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/cdproto/network"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

type pageState struct {
	mu sync.Mutex

	navigation  int64
	mainFrame   cdp.FrameID
	console     []ConsoleMessage
	dialog      *Dialog
	fileChooser cdp.BackendNodeID
	requests    []*NetworkRequest
	requestByID map[network.RequestID]*NetworkRequest
	refs        map[string]cdp.BackendNodeID
}

func newPageState() *pageState {
	return &pageState{
		requestByID: make(map[network.RequestID]*NetworkRequest),
		refs:        make(map[string]cdp.BackendNodeID),
	}
}

func (p *Page) observerActions() []chromedp.Action {
	return []chromedp.Action{
		chromedp.ActionFunc(func(ctx context.Context) error { return network.Enable().WithMaxPostDataSize(1024 * 1024).Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error { return log.Enable().Do(ctx) }),
		chromedp.ActionFunc(func(ctx context.Context) error { return cdppage.SetInterceptFileChooserDialog(true).Do(ctx) }),
	}
}

func (p *Page) initObservers() {
	chromedp.ListenTarget(p.ctx, func(event any) {
		s := p.state
		s.mu.Lock()
		defer s.mu.Unlock()

		switch e := event.(type) {
		case *runtime.EventConsoleAPICalled:
			args := make([]string, 0, len(e.Args))
			for _, arg := range e.Args {
				args = append(args, remoteObjectText(arg))
			}
			s.console = append(s.console, ConsoleMessage{
				Level:      consoleLevel(string(e.Type)),
				Text:       strings.Join(args, " "),
				Timestamp:  time.Now(),
				Navigation: s.navigation,
			})
		case *runtime.EventExceptionThrown:
			text := "Uncaught exception"
			if e.ExceptionDetails != nil {
				text = e.ExceptionDetails.Text
				if e.ExceptionDetails.Exception != nil && e.ExceptionDetails.Exception.Description != "" {
					text = e.ExceptionDetails.Exception.Description
				}
			}
			s.console = append(s.console, ConsoleMessage{Level: ConsoleError, Text: text, Timestamp: time.Now(), Navigation: s.navigation})
		case *log.EventEntryAdded:
			if e.Entry != nil {
				s.console = append(s.console, ConsoleMessage{
					Level:      consoleLevel(string(e.Entry.Level)),
					Text:       e.Entry.Text,
					Timestamp:  time.Now(),
					Navigation: s.navigation,
				})
			}
		case *cdppage.EventFrameNavigated:
			if e.Frame != nil && e.Frame.ParentID == "" {
				s.mainFrame = e.Frame.ID
				s.navigation++
				s.refs = make(map[string]cdp.BackendNodeID)
			}
		case *cdppage.EventJavascriptDialogOpening:
			s.dialog = &Dialog{Type: string(e.Type), Message: e.Message, DefaultPrompt: e.DefaultPrompt, URL: e.URL}
		case *cdppage.EventJavascriptDialogClosed:
			s.dialog = nil
		case *cdppage.EventFileChooserOpened:
			s.fileChooser = e.BackendNodeID
		case *network.EventRequestWillBeSent:
			if e.Request == nil {
				break
			}
			if e.RedirectResponse != nil {
				if previous := s.requestByID[e.RequestID]; previous != nil {
					previous.Response = cloneResponse(e.RedirectResponse)
					previous.Finished = true
				}
			}
			r := &NetworkRequest{
				ID:             e.RequestID,
				URL:            e.Request.URL,
				Method:         e.Request.Method,
				ResourceType:   string(e.Type),
				RequestHeaders: cloneHeaders(e.Request.Headers),
				HasPostData:    e.Request.HasPostData,
				StartedAt:      time.Now(),
			}
			s.requests = append(s.requests, r)
			s.requestByID[e.RequestID] = r
		case *network.EventResponseReceived:
			if r := s.requestByID[e.RequestID]; r != nil && e.Response != nil {
				r.Response = cloneResponse(e.Response)
				r.ResourceType = string(e.Type)
			}
		case *network.EventLoadingFinished:
			if r := s.requestByID[e.RequestID]; r != nil {
				r.Finished = true
				r.Duration = time.Since(r.StartedAt)
			}
		case *network.EventLoadingFailed:
			if r := s.requestByID[e.RequestID]; r != nil {
				r.Finished = true
				r.Duration = time.Since(r.StartedAt)
				r.Failure = e.ErrorText
			}
		}
	})
}

func remoteObjectText(object *runtime.RemoteObject) string {
	if object == nil {
		return "undefined"
	}
	if len(object.Value) != 0 {
		var value any
		if json.Unmarshal(object.Value, &value) == nil {
			if text, ok := value.(string); ok {
				return text
			}
			data, _ := json.Marshal(value)
			return string(data)
		}
	}
	if object.Description != "" {
		return object.Description
	}
	return fmt.Sprint(object.Type)
}

func consoleLevel(level string) ConsoleLevel {
	switch strings.ToLower(level) {
	case "error", "assert":
		return ConsoleError
	case "warning", "warn":
		return ConsoleWarning
	case "debug", "verbose":
		return ConsoleDebug
	default:
		return ConsoleInfo
	}
}

func cloneHeaders(headers network.Headers) map[string]string {
	result := make(map[string]string, len(headers))
	for key, value := range headers {
		result[key] = fmt.Sprint(value)
	}
	return result
}

func cloneResponse(response *network.Response) *NetworkResponse {
	if response == nil {
		return nil
	}
	return &NetworkResponse{
		Status:     response.Status,
		StatusText: response.StatusText,
		Headers:    cloneHeaders(response.Headers),
		MimeType:   response.MimeType,
	}
}
