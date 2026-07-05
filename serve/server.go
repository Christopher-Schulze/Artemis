package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/Christopher-Schulze/Artemis/agent"
	"github.com/Christopher-Schulze/Artemis/engine"
)

// Opts configures a Server.
type Opts struct {
	// Logger receives connection lifecycle and command-level logs.
	Logger *slog.Logger
	// AcceptOptions tunes the websocket Accept handshake.
	AcceptOptions *websocket.AcceptOptions
}

// Server is a single-engine WebSocket steering server. Multiple
// concurrent sessions can be active per server.
type Server struct {
	eng      *engine.Engine
	opts     Opts
	mu       sync.Mutex
	sessions map[string]*session
	nextID   atomic.Uint64
	srv      *http.Server
}

type session struct {
	id    string
	mu    sync.Mutex
	pages map[string]*engine.Page
}

// New creates a Server bound to eng. The returned Server must be Closed
// after Serve returns.
func New(eng *engine.Engine, opts Opts) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Server{
		eng:      eng,
		opts:     opts,
		sessions: make(map[string]*session),
	}
}

// ListenAndServe blocks while serving on addr until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	s.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Close stops the server.
func (s *Server) Close() error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Close()
}

// HandleWSForTest exposes the WebSocket handler for in-process tests
// that want to drive the server without spawning a subprocess.
func (s *Server) HandleWSForTest(w http.ResponseWriter, r *http.Request) {
	s.handleWS(w, r)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, s.opts.AcceptOptions)
	if err != nil {
		s.opts.Logger.Warn("ws accept failed", "err", err)
		return
	}
	defer c.CloseNow()
	// Page dumps (especially HTML) can easily exceed the default 32KB
	// read limit. Allow up to 8MB per message.
	c.SetReadLimit(8 << 20)

	ctx := r.Context()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			s.writeResp(ctx, c, &Response{
				ID: req.ID, OK: false,
				Error: &Err{Code: "bad_request", Message: err.Error()},
			})
			continue
		}
		resp := s.dispatch(ctx, &req)
		s.writeResp(ctx, c, resp)
	}
}

func (s *Server) writeResp(ctx context.Context, c *websocket.Conn, r *Response) {
	out, err := json.Marshal(r)
	if err != nil {
		s.opts.Logger.Error("marshal resp", "err", err)
		return
	}
	if err := c.Write(ctx, websocket.MessageText, out); err != nil {
		s.opts.Logger.Warn("ws write", "err", err)
	}
}

func (s *Server) dispatch(ctx context.Context, req *Request) *Response {
	switch req.Cmd {
	case "version":
		return okResp(req.ID, VersionResponse{
			Protocol: ProtocolVersion,
			Server:   "artemis-serve",
		})
	case "session.new":
		return s.cmdSessionNew(req)
	case "session.close":
		return s.cmdSessionClose(req)
	case "page.open":
		return s.cmdPageOpen(ctx, req)
	case "page.close":
		return s.cmdPageClose(req)
	case "page.eval":
		return s.cmdPageEval(ctx, req)
	case "page.dump":
		return s.cmdPageDump(req)
	case "page.click_by_text":
		return s.cmdPageClickByText(ctx, req)
	case "page.type":
		return s.cmdPageType(req)
	case "page.wait_idle":
		return s.cmdPageWaitIdle(ctx, req)
	case "page.assert":
		return s.cmdPageAssert(ctx, req)
	default:
		return errResp(req.ID, "unknown_cmd", fmt.Sprintf("unknown cmd %q", req.Cmd))
	}
}

func (s *Server) newSessionID() string {
	return strconv.FormatUint(s.nextID.Add(1), 10)
}

func (s *Server) cmdSessionNew(req *Request) *Response {
	id := "s" + s.newSessionID()
	sess := &session{id: id, pages: make(map[string]*engine.Page)}
	s.mu.Lock()
	s.sessions[id] = sess
	s.mu.Unlock()
	return okResp(req.ID, map[string]any{"sessionId": id})
}

func (s *Server) cmdSessionClose(req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	s.mu.Lock()
	sess := s.sessions[p.SessionID]
	delete(s.sessions, p.SessionID)
	s.mu.Unlock()
	if sess != nil {
		sess.mu.Lock()
		for _, page := range sess.pages {
			_ = page.Close()
		}
		sess.mu.Unlock()
	}
	return okResp(req.ID, map[string]any{})
}

func (s *Server) lookupSession(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) cmdPageOpen(ctx context.Context, req *Request) *Response {
	var p struct {
		SessionID  string `json:"sessionId"`
		URL        string `json:"url"`
		RunScripts bool   `json:"runScripts"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	sess := s.lookupSession(p.SessionID)
	if sess == nil {
		return errResp(req.ID, "no_session", "unknown sessionId")
	}
	page, err := s.eng.Fetch(ctx, p.URL, engine.FetchOpts{RunInlineScripts: p.RunScripts})
	if err != nil {
		return errResp(req.ID, "fetch_failed", err.Error())
	}
	pageID := "p" + s.newSessionID()
	sess.mu.Lock()
	sess.pages[pageID] = page
	sess.mu.Unlock()
	return okResp(req.ID, map[string]any{
		"pageId": pageID,
		"url":    page.URL(),
		"status": page.StatusCode(),
		"title":  page.Title(),
	})
}

func (s *Server) cmdPageClose(req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	sess := s.lookupSession(p.SessionID)
	if sess == nil {
		return errResp(req.ID, "no_session", "")
	}
	sess.mu.Lock()
	page := sess.pages[p.PageID]
	delete(sess.pages, p.PageID)
	sess.mu.Unlock()
	if page != nil {
		_ = page.Close()
	}
	return okResp(req.ID, map[string]any{})
}

func (s *Server) lookupPage(sessionID, pageID string) *engine.Page {
	sess := s.lookupSession(sessionID)
	if sess == nil {
		return nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	return sess.pages[pageID]
}

func (s *Server) cmdPageEval(ctx context.Context, req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
		Expr      string `json:"expr"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	v, err := page.Eval(ctx, p.Expr)
	if err != nil {
		return errResp(req.ID, "eval_failed", err.Error())
	}
	return okResp(req.ID, map[string]any{"value": v.String()})
}

func (s *Server) cmdPageDump(req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
		Format    string `json:"format"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	switch p.Format {
	case "html":
		return okResp(req.ID, map[string]any{"data": page.HTML()})
	case "markdown", "":
		return okResp(req.ID, map[string]any{"data": page.Markdown()})
	case "text":
		return okResp(req.ID, map[string]any{"data": page.Text()})
	case "title":
		return okResp(req.ID, map[string]any{"data": page.Title()})
	case "links":
		return okResp(req.ID, map[string]any{"data": page.Links()})
	case "structured":
		return okResp(req.ID, map[string]any{"data": page.StructuredData()})
	case "semantic":
		return okResp(req.ID, map[string]any{"data": agent.SemanticString(page.SemanticTree())})
	default:
		return errResp(req.ID, "bad_format", "unknown format "+p.Format)
	}
}

func (s *Server) cmdPageClickByText(ctx context.Context, req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	n, ok := agent.ClickByText(page.Document(), p.Text)
	if !ok {
		return errResp(req.ID, "not_found", "no clickable element with that text")
	}
	if err := page.Click(ctx, n); err != nil {
		return errResp(req.ID, "click_failed", err.Error())
	}
	return okResp(req.ID, map[string]any{})
}

func (s *Server) cmdPageType(req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
		Selector  string `json:"selector"`
		Text      string `json:"text"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	if err := agent.Type(page.Document(), p.Selector, p.Text); err != nil {
		return errResp(req.ID, "type_failed", err.Error())
	}
	return okResp(req.ID, map[string]any{})
}

func (s *Server) cmdPageWaitIdle(ctx context.Context, req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	if err := page.WaitIdle(ctx); err != nil {
		return errResp(req.ID, "wait_failed", err.Error())
	}
	return okResp(req.ID, map[string]any{})
}

// cmdPageAssert evaluates an assertion against the current page state.
// Supported modes:
//   - mode=selector_exists   params: selector, want (true|false, default true)
//   - mode=title_contains    params: substring
//   - mode=text_contains     params: substring
//   - mode=url_contains      params: substring
//   - mode=status_eq         params: status (int)
//   - mode=eval_truthy       params: expr (JS)
func (s *Server) cmdPageAssert(ctx context.Context, req *Request) *Response {
	var p struct {
		SessionID string `json:"sessionId"`
		PageID    string `json:"pageId"`
		Mode      string `json:"mode"`
		Selector  string `json:"selector"`
		Want      *bool  `json:"want"`
		Substring string `json:"substring"`
		Status    int    `json:"status"`
		Expr      string `json:"expr"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		return errResp(req.ID, "bad_params", err.Error())
	}
	page := s.lookupPage(p.SessionID, p.PageID)
	if page == nil {
		return errResp(req.ID, "no_page", "")
	}
	want := true
	if p.Want != nil {
		want = *p.Want
	}
	type assertionResult struct {
		Pass bool   `json:"pass"`
		Got  string `json:"got"`
	}
	switch p.Mode {
	case "selector_exists":
		n, err := page.Document().QuerySelector(p.Selector)
		if err != nil {
			return errResp(req.ID, "assert_failed", "query: "+err.Error())
		}
		got := n != nil
		return okResp(req.ID, assertionResult{Pass: got == want, Got: fmt.Sprintf("exists=%v", got)})
	case "title_contains":
		t := page.Title()
		got := strings.Contains(t, p.Substring)
		return okResp(req.ID, assertionResult{Pass: got == want, Got: t})
	case "text_contains":
		t := page.Text()
		got := strings.Contains(t, p.Substring)
		return okResp(req.ID, assertionResult{Pass: got == want, Got: truncate(t, 200)})
	case "url_contains":
		u := page.URL()
		got := strings.Contains(u, p.Substring)
		return okResp(req.ID, assertionResult{Pass: got == want, Got: u})
	case "status_eq":
		got := page.StatusCode()
		return okResp(req.ID, assertionResult{Pass: got == p.Status, Got: strconv.Itoa(got)})
	case "eval_truthy":
		if p.Expr == "" {
			return errResp(req.ID, "bad_params", "eval_truthy requires expr")
		}
		v, err := page.Eval(ctx, p.Expr)
		if err != nil {
			return errResp(req.ID, "assert_failed", "eval: "+err.Error())
		}
		got := v != nil && v.Bool()
		return okResp(req.ID, assertionResult{Pass: got == want, Got: v.String()})
	default:
		return errResp(req.ID, "bad_mode", "unknown assert mode "+p.Mode)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func okResp(id string, value any) *Response {
	return &Response{ID: id, OK: true, Value: value}
}

func errResp(id, code, msg string) *Response {
	return &Response{ID: id, OK: false, Error: &Err{Code: code, Message: msg}}
}
