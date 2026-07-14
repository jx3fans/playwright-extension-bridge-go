package relay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"

	"github.com/coder/websocket"

	"github.com/jx3fans/playwright-extension-bridge-go/internal/protocol"
)

type Server struct {
	handler *protocol.Handler

	listener net.Listener
	http     *http.Server
	host     string

	extensionPath string
	cdpPath       string

	mu            sync.Mutex
	extensionConn *ExtensionConn
	cdpConn       *websocket.Conn
	closed        bool
}

func NewServer(handler *protocol.Handler) (*Server, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	id, err := randomID()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	s := &Server{
		handler:       handler,
		listener:      listener,
		host:          "ws://" + listener.Addr().String(),
		extensionPath: "/extension/" + id,
		cdpPath:       "/cdp/" + id,
	}
	mux := http.NewServeMux()
	mux.HandleFunc(s.extensionPath, s.handleExtension)
	mux.HandleFunc(s.cdpPath, s.handleCDP)
	s.http = &http.Server{Handler: mux}
	return s, nil
}

func (s *Server) Start() {
	go func() {
		if err := s.http.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.handler.ExtensionDisconnected(err)
		}
	}()
}

func (s *Server) ExtensionEndpoint() string { return s.host + s.extensionPath }

func (s *Server) CDPEndpoint() string { return s.host + s.cdpPath }

func (s *Server) Close(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	extension := s.extensionConn
	cdp := s.cdpConn
	s.mu.Unlock()
	if cdp != nil {
		_ = cdp.Close(websocket.StatusNormalClosure, "bridge closed")
	}
	if extension != nil {
		_ = extension.Close(websocket.StatusNormalClosure, "bridge closed")
	}
	return s.http.Shutdown(ctx)
}

func (s *Server) handleExtension(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	if s.closed || s.extensionConn != nil {
		s.mu.Unlock()
		http.Error(w, "extension connection unavailable", http.StatusConflict)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.mu.Unlock()
		return
	}
	conn.SetReadLimit(16 << 20)
	extension := NewExtensionConn(conn, s.handler.HandleExtensionEvent, func(err error) {
		s.onExtensionClosed(err)
	})
	if err := s.handler.SetExtension(extension); err != nil {
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	s.extensionConn = extension
	s.mu.Unlock()
	_ = extension.Run(r.Context())
}

func (s *Server) handleCDP(w http.ResponseWriter, r *http.Request) {
	if !s.handler.IsReady() {
		http.Error(w, "extension not initialized", http.StatusServiceUnavailable)
		return
	}
	s.mu.Lock()
	if s.closed || s.cdpConn != nil {
		s.mu.Unlock()
		http.Error(w, "CDP connection unavailable", http.StatusConflict)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		s.mu.Unlock()
		return
	}
	conn.SetReadLimit(64 << 20)
	writer := &cdpWriter{conn: conn}
	if err := s.handler.SetCDPEmitter(writer.Write); err != nil {
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	s.cdpConn = conn
	s.mu.Unlock()

	ctx := r.Context()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			break
		}
		var req protocol.CDPMessage
		if err := json.Unmarshal(data, &req); err != nil {
			continue
		}
		go func() {
			resp := s.handler.HandleCDP(ctx, req)
			_ = writer.Write(resp)
		}()
	}
	s.handler.ClearCDPEmitter()
	s.mu.Lock()
	if s.cdpConn == conn {
		s.cdpConn = nil
	}
	extension := s.extensionConn
	s.mu.Unlock()
	if extension != nil {
		_ = extension.Close(websocket.StatusNormalClosure, "CDP client disconnected")
	}
}

func (s *Server) onExtensionClosed(err error) {
	s.handler.ExtensionDisconnected(err)
	s.mu.Lock()
	s.extensionConn = nil
	cdp := s.cdpConn
	s.mu.Unlock()
	if cdp != nil {
		_ = cdp.Close(websocket.StatusGoingAway, "extension disconnected")
	}
}

type cdpWriter struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (w *cdpWriter) Write(message protocol.CDPMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.Write(context.Background(), websocket.MessageText, payload)
}

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate relay id: %w", err)
	}
	return hex.EncodeToString(b), nil
}
