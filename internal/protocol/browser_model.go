package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

type sendExtension func(context.Context, string, any) (json.RawMessage, error)

type tabSession struct {
	tabID         int
	sessionID     string
	targetInfo    map[string]any
	childSessions map[string]struct{}
}

type attachCall struct {
	done    chan struct{}
	session *tabSession
	err     error
}

// BrowserModel owns all tab-to-session routing state.
type BrowserModel struct {
	mu sync.RWMutex

	send sendExtension
	emit func(CDPMessage)

	knownTabs  map[int]Tab
	sessions   map[int]*tabSession
	attaching  map[int]*attachCall
	autoAttach bool
	nextID     int
}

func NewBrowserModel(send sendExtension, emit func(CDPMessage)) *BrowserModel {
	return &BrowserModel{
		send:      send,
		emit:      emit,
		knownTabs: make(map[int]Tab),
		sessions:  make(map[int]*tabSession),
		attaching: make(map[int]*attachCall),
		nextID:    1,
	}
}

func (m *BrowserModel) OnTabCreated(tab Tab) {
	if tab.ID == nil {
		return
	}
	tabID := *tab.ID
	m.mu.Lock()
	m.knownTabs[tabID] = tab
	autoAttach := m.autoAttach
	m.mu.Unlock()
	if autoAttach {
		go func() { _, _ = m.attachTab(context.Background(), tabID) }()
	}
}

func (m *BrowserModel) OnTabRemoved(tabID int) {
	m.mu.Lock()
	delete(m.knownTabs, tabID)
	s := m.detachTabLocked(tabID)
	m.mu.Unlock()
	m.emitDetached(s)
}

func (m *BrowserModel) detachTab(tabID int) {
	m.mu.Lock()
	s := m.detachTabLocked(tabID)
	m.mu.Unlock()
	m.emitDetached(s)
}

func (m *BrowserModel) detachTabLocked(tabID int) *tabSession {
	s := m.sessions[tabID]
	delete(m.sessions, tabID)
	return s
}

func (m *BrowserModel) emitDetached(s *tabSession) {
	if s != nil {
		m.emit(CDPMessage{
			Method: "Target.detachedFromTarget",
			Params: mustMarshalRaw(map[string]any{
				"sessionId": s.sessionID,
				"targetId":  stringValue(s.targetInfo["targetId"]),
			}),
		})
	}
}

func (m *BrowserModel) OnDebuggerDetach(source debuggerSource) {
	if source.TabID != nil {
		m.detachTab(*source.TabID)
	}
}

func (m *BrowserModel) OnDebuggerEvent(source debuggerSource, method string, params json.RawMessage) {
	if source.TabID == nil {
		return
	}
	m.mu.Lock()
	s := m.sessions[*source.TabID]
	if s == nil {
		m.mu.Unlock()
		return
	}
	var child struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(params, &child)
	if method == "Target.attachedToTarget" && child.SessionID != "" {
		s.childSessions[child.SessionID] = struct{}{}
	} else if method == "Target.detachedFromTarget" && child.SessionID != "" {
		delete(s.childSessions, child.SessionID)
	}
	sessionID := source.SessionID
	if sessionID == "" {
		sessionID = s.sessionID
	}
	m.mu.Unlock()
	m.emit(CDPMessage{SessionID: sessionID, Method: method, Params: params})
}

func (m *BrowserModel) EnableAutoAttach(ctx context.Context) error {
	m.mu.Lock()
	m.autoAttach = true
	ids := make([]int, 0, len(m.knownTabs))
	for id := range m.knownTabs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		if _, err := m.attachTab(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *BrowserModel) CreateTarget(ctx context.Context, url string) (json.RawMessage, error) {
	raw, err := m.send(ctx, "chrome.tabs.create", []any{map[string]any{"url": url}})
	if err != nil {
		return nil, err
	}
	var tab Tab
	if err := json.Unmarshal(raw, &tab); err != nil {
		return nil, fmt.Errorf("decode created tab: %w", err)
	}
	if tab.ID == nil {
		return nil, errors.New("extension returned a tab without id")
	}
	m.OnTabCreated(tab)
	s, err := m.attachTab(ctx, *tab.ID)
	if err != nil {
		return nil, err
	}
	return marshalRaw(map[string]any{"targetId": stringValue(s.targetInfo["targetId"])})
}

func (m *BrowserModel) CloseTarget(ctx context.Context, targetID string) (json.RawMessage, error) {
	s := m.findSession(func(s *tabSession) bool { return stringValue(s.targetInfo["targetId"]) == targetID })
	if s == nil {
		return marshalRaw(map[string]any{"success": false})
	}
	if _, err := m.send(ctx, "chrome.tabs.remove", []any{s.tabID}); err != nil {
		return nil, err
	}
	return marshalRaw(map[string]any{"success": true})
}

func (m *BrowserModel) AttachToTarget(ctx context.Context, targetID string) (json.RawMessage, error) {
	s := m.findSession(func(s *tabSession) bool { return stringValue(s.targetInfo["targetId"]) == targetID })
	if s == nil {
		return nil, fmt.Errorf("target not found: %s", targetID)
	}
	return marshalRaw(map[string]any{"sessionId": s.sessionID})
}

func (m *BrowserModel) IsTopLevelSession(sessionID string) bool {
	return m.findSession(func(s *tabSession) bool { return s.sessionID == sessionID }) != nil
}

func (m *BrowserModel) GetTargetInfo(sessionID, targetID string) (json.RawMessage, error) {
	s := m.findSession(func(s *tabSession) bool {
		return (sessionID != "" && s.sessionID == sessionID) || (targetID != "" && stringValue(s.targetInfo["targetId"]) == targetID)
	})
	if s == nil {
		return nil, errors.New("target info not found")
	}
	return marshalRaw(map[string]any{"targetInfo": s.targetInfo})
}

func (m *BrowserModel) GetTargets() (json.RawMessage, error) {
	m.mu.RLock()
	infos := make([]map[string]any, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, cloneMap(s.targetInfo))
	}
	m.mu.RUnlock()
	return marshalRaw(map[string]any{"targetInfos": infos})
}

func (m *BrowserModel) EmitTargetCreatedEvents() {
	m.mu.RLock()
	infos := make([]map[string]any, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, cloneMap(s.targetInfo))
	}
	m.mu.RUnlock()
	for _, info := range infos {
		m.emit(CDPMessage{Method: "Target.targetCreated", Params: mustMarshalRaw(map[string]any{"targetInfo": info})})
	}
}

func (m *BrowserModel) Forward(ctx context.Context, sessionID, method string, params json.RawMessage) (json.RawMessage, error) {
	var s *tabSession
	var childSessionID string
	if sessionID == "" {
		m.mu.RLock()
		for _, candidate := range m.sessions {
			s = candidate
			break
		}
		m.mu.RUnlock()
		if s == nil {
			return nil, fmt.Errorf("no attached tab for browser-level command %s", method)
		}
	} else {
		s = m.findSession(func(s *tabSession) bool { return s.sessionID == sessionID })
		if s == nil {
			s = m.findSession(func(s *tabSession) bool {
				_, ok := s.childSessions[sessionID]
				return ok
			})
			childSessionID = sessionID
		}
		if s == nil {
			return nil, fmt.Errorf("no tab found for sessionId: %s", sessionID)
		}
	}
	target := map[string]any{"tabId": s.tabID}
	if childSessionID != "" {
		target["sessionId"] = childSessionID
	}
	args := []any{target, method}
	if len(params) != 0 && string(params) != "null" {
		args = append(args, json.RawMessage(params))
	}
	return m.send(ctx, "chrome.debugger.sendCommand", args)
}

func (m *BrowserModel) attachTab(ctx context.Context, tabID int) (*tabSession, error) {
	m.mu.Lock()
	if s := m.sessions[tabID]; s != nil {
		m.mu.Unlock()
		return s, nil
	}
	if call := m.attaching[tabID]; call != nil {
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return call.session, call.err
		}
	}
	call := &attachCall{done: make(chan struct{})}
	m.attaching[tabID] = call
	m.mu.Unlock()

	s, err := m.doAttachTab(ctx, tabID)
	m.mu.Lock()
	if err == nil {
		m.sessions[tabID] = s
	}
	call.session, call.err = s, err
	delete(m.attaching, tabID)
	close(call.done)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.emit(CDPMessage{
		Method: "Target.attachedToTarget",
		Params: mustMarshalRaw(map[string]any{
			"sessionId":          s.sessionID,
			"targetInfo":         withAttached(s.targetInfo),
			"waitingForDebugger": false,
		}),
	})
	return s, nil
}

func (m *BrowserModel) doAttachTab(ctx context.Context, tabID int) (*tabSession, error) {
	if _, err := m.send(ctx, "chrome.debugger.attach", []any{map[string]any{"tabId": tabID}, "1.3"}); err != nil {
		return nil, err
	}
	raw, err := m.send(ctx, "chrome.debugger.sendCommand", []any{map[string]any{"tabId": tabID}, "Target.getTargetInfo"})
	if err != nil {
		_, _ = m.send(context.Background(), "chrome.debugger.detach", []any{map[string]any{"tabId": tabID}})
		return nil, err
	}
	var result struct {
		TargetInfo map[string]any `json:"targetInfo"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		_, _ = m.send(context.Background(), "chrome.debugger.detach", []any{map[string]any{"tabId": tabID}})
		return nil, fmt.Errorf("decode target info: %w", err)
	}
	if stringValue(result.TargetInfo["targetId"]) == "" {
		_, _ = m.send(context.Background(), "chrome.debugger.detach", []any{map[string]any{"tabId": tabID}})
		return nil, errors.New("Target.getTargetInfo returned no targetId")
	}
	m.mu.Lock()
	sessionID := fmt.Sprintf("pw-tab-%d", m.nextID)
	m.nextID++
	m.mu.Unlock()
	return &tabSession{
		tabID:         tabID,
		sessionID:     sessionID,
		targetInfo:    result.TargetInfo,
		childSessions: make(map[string]struct{}),
	}, nil
}

func (m *BrowserModel) findSession(match func(*tabSession) bool) *tabSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.sessions {
		if match(s) {
			return s
		}
	}
	return nil
}

func mustMarshalRaw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func cloneMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func withAttached(src map[string]any) map[string]any {
	dst := cloneMap(src)
	dst["attached"] = true
	return dst
}
