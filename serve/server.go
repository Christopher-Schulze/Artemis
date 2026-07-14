package serve

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/profile"
)

// Opts configures a Server.
type Opts struct {
	// Logger receives connection lifecycle and command-level logs.
	Logger *slog.Logger
	// AcceptOptions tunes the websocket Accept handshake.
	AcceptOptions *websocket.AcceptOptions
	// AuthToken is the optional bearer token required for every connection.
	// If empty, no authentication is enforced.
	AuthToken string
	// OriginPatterns lists allowed WebSocket origins. Empty means the
	// websocket Accept default (same-origin / no cross-origin).
	OriginPatterns []string
	// InsecureSkipOrigin disables origin verification.
	InsecureSkipOrigin bool
	// RateLimit is the optional per-connection request rate limit.
	RateLimit RateLimit
}

// RateLimit configures per-connection request throttling.
type RateLimit struct {
	RequestsPerSecond int
	Burst             int
}

// Server is a single Agent WebSocket steering server. Multiple concurrent
// sessions can be active per server.
type Server struct {
	agent       *artemis.Agent
	opts        Opts
	mu          sync.Mutex
	writeMu     sync.Mutex
	nextSeq     atomic.Uint64
	srv         *http.Server
	authTokenMu sync.RWMutex
	authToken   string
	inflight    map[string]context.CancelFunc
	outbox      map[string]*streamOutbox
}

// streamOutbox stores ordered events and a terminal response for a stream so
// that a reconnecting client can resume without duplication.
type streamOutbox struct {
	mu       sync.Mutex
	events   []streamEvent
	terminal *Response
	closed   bool
	seq      uint64
	reqID    string
}

type streamEvent struct {
	seq      uint64
	event    *Event
	terminal *Response
}

// New creates a Server bound to an Agent. The returned Server must be Closed
// after Serve returns.
func New(agent *artemis.Agent, opts Opts) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.AcceptOptions == nil {
		opts.AcceptOptions = &websocket.AcceptOptions{}
	}
	if len(opts.OriginPatterns) > 0 {
		opts.AcceptOptions.OriginPatterns = opts.OriginPatterns
	}
	opts.AcceptOptions.InsecureSkipVerify = opts.InsecureSkipOrigin
	s := &Server{
		agent:     agent,
		opts:      opts,
		authToken: opts.AuthToken,
		inflight:  make(map[string]context.CancelFunc),
		outbox:    make(map[string]*streamOutbox),
	}
	return s
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
	if token := s.getAuthToken(); token != "" {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

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
	var wg sync.WaitGroup
	rl := newRateLimiter(s.opts.RateLimit)

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			wg.Add(1)
			go func(req Request) {
				defer wg.Done()
				s.writeResp(ctx, c, &Response{
					ID: req.ID, OK: false,
					Error: &Err{Code: string(ErrBadRequest), Message: err.Error()},
				})
			}(req)
			continue
		}
		wg.Add(1)
		go s.handleRequest(ctx, c, &req, &wg, rl)
	}
	wg.Wait()
}

func (s *Server) handleRequest(ctx context.Context, c *websocket.Conn, req *Request, wg *sync.WaitGroup, rl *rateLimiter) {
	defer wg.Done()
	if rl != nil && !rl.Allow() {
		s.writeResp(ctx, c, errResp(req.ID, string(ErrRateExceeded), "rate limit exceeded"))
		return
	}
	if verr := s.checkVersion(req); verr != nil {
		s.writeResp(ctx, c, verr)
		return
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	s.registerInflight(req.ID, cancel)
	defer func() {
		cancel()
		s.unregisterInflight(req.ID)
	}()
	resp := s.dispatch(dispatchCtx, ctx, c, req, wg)
	if resp != nil {
		s.writeResp(ctx, c, resp)
	}
}

func (s *Server) writeResp(ctx context.Context, c *websocket.Conn, r *Response) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	r.Version = ProtocolVersion
	r.Seq = int64(s.nextSeq.Add(1))
	out, err := json.Marshal(r)
	if err != nil {
		s.opts.Logger.Error("marshal resp", "err", err)
		return
	}
	if err := c.Write(ctx, websocket.MessageText, out); err != nil {
		s.opts.Logger.Warn("ws write", "err", err)
	}
}

func (s *Server) writeEvent(ctx context.Context, c *websocket.Conn, ev *Event) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ev.Version = ProtocolVersion
	ev.Seq = int64(s.nextSeq.Add(1))
	out, err := json.Marshal(ev)
	if err != nil {
		s.opts.Logger.Error("marshal event", "err", err)
		return
	}
	if err := c.Write(ctx, websocket.MessageText, out); err != nil {
		s.opts.Logger.Warn("ws event", "err", err)
	}
}

func (s *Server) dispatch(ctx, connCtx context.Context, c *websocket.Conn, req *Request, wg *sync.WaitGroup) *Response {
	switch req.Cmd {
	case string(CmdVersion):
		return s.cmdVersion(req)
	case string(CmdCapabilities):
		return s.cmdCapabilities(req)
	case string(CmdHeartbeat):
		return s.cmdHeartbeat(req)
	case string(CmdSessionNew):
		return s.cmdSessionNew(ctx, req)
	case string(CmdSessionClose):
		return s.cmdSessionClose(req)
	case string(CmdSessionList):
		return s.cmdSessionList(req)
	case string(CmdPageOpen):
		return s.cmdPageOpen(ctx, req)
	case string(CmdPageClose):
		return s.cmdPageClose(req)
	case string(CmdPageEval):
		return s.cmdPageEval(ctx, req)
	case string(CmdPageDump):
		return s.cmdPageDump(req)
	case string(CmdPageClickByText):
		return s.cmdPageClickByText(ctx, req)
	case string(CmdPageType):
		return s.cmdPageType(req)
	case string(CmdPageWaitIdle):
		return s.cmdPageWaitIdle(ctx, req)
	case string(CmdPageAssert):
		return s.cmdPageAssert(ctx, req)
	case string(CmdChromiumAct):
		return s.cmdChromiumAct(ctx, req)
	case string(CmdStream):
		return s.cmdStream(ctx, connCtx, c, req, wg)
	case string(CmdCancel):
		return s.cmdCancel(req)
	case string(CmdTokenRotate):
		return s.cmdTokenRotate(req)
	default:
		return errResp(req.ID, string(ErrUnknownCmd), fmt.Sprintf("unknown cmd %q", req.Cmd))
	}
}

func (s *Server) registerInflight(id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight[id] = cancel
}

func (s *Server) unregisterInflight(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, id)
}

func (s *Server) cancelRequest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn, ok := s.inflight[id]
	if ok && fn != nil {
		fn()
	}
	return ok
}

func (s *Server) getAuthToken() string {
	s.authTokenMu.RLock()
	defer s.authTokenMu.RUnlock()
	return s.authToken
}

func (s *Server) setAuthToken(token string) {
	s.authTokenMu.Lock()
	defer s.authTokenMu.Unlock()
	s.authToken = token
}

func (s *Server) checkVersion(req *Request) *Response {
	if req.Cmd == string(CmdVersion) || req.Version == "" {
		return nil
	}
	if majorVersion(req.Version) != majorVersion(ProtocolVersion) {
		return errResp(req.ID, string(ErrVersionMismatch), fmt.Sprintf("protocol version mismatch: client %s, server %s", req.Version, ProtocolVersion))
	}
	return nil
}

func majorVersion(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) == 0 {
		return v
	}
	return parts[0]
}

func (s *Server) taskErrorResponse(req *Request, err error, fallback ErrCode) *Response {
	var taskErr *artemis.TaskError
	if !errors.As(err, &taskErr) {
		return errResp(req.ID, string(fallback), err.Error())
	}
	return errResp(req.ID, string(mapTaskErrorCode(taskErr, fallback)), taskErr.Error())
}

func mapTaskErrorCode(err *artemis.TaskError, fallback ErrCode) ErrCode {
	switch err.Code {
	case artemis.TaskErrorInvalidInput:
		return ErrBadParams
	case artemis.TaskErrorCapabilityUnavailable:
		return ErrCapabilityUnavailable
	case artemis.TaskErrorPolicyDenied:
		return ErrOwnershipDenied
	case artemis.TaskErrorTimeout:
		return fallback
	case artemis.TaskErrorCancelled:
		return ErrCancelled
	case artemis.TaskErrorStaleReference, artemis.TaskErrorInvalidTransition:
		return ErrNoSession
	case artemis.TaskErrorSessionNotFound:
		return ErrNoSession
	case artemis.TaskErrorSessionLimit:
		return ErrSessionLimit
	case artemis.TaskErrorResourceLimit:
		return ErrResourceLimit
	case artemis.TaskErrorPageNotFound:
		return ErrNoPage
	case artemis.TaskErrorExecutionFailed:
		return fallback
	case artemis.TaskErrorBrowserCrash:
		return fallback
	default:
		return fallback
	}
}

func (s *Server) cmdVersion(req *Request) *Response {
	return okResp(req.ID, VersionResponse{
		Protocol:     ProtocolVersion,
		Server:       "artemis-serve",
		Capabilities: artemis.Capabilities(),
	})
}

func (s *Server) cmdCapabilities(req *Request) *Response {
	return okResp(req.ID, VersionResponse{
		Protocol:     ProtocolVersion,
		Server:       "artemis-serve",
		Capabilities: artemis.Capabilities(),
	})
}

func (s *Server) cmdHeartbeat(req *Request) *Response {
	return okResp(req.ID, HeartbeatResult{Now: time.Now().UnixMilli()})
}

func (s *Server) cmdSessionNew(ctx context.Context, req *Request) *Response {
	var params SessionNewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, string(ErrBadParams), err.Error())
		}
	}
	owner := params.OwnerUserRef
	if owner == "" {
		owner = "anonymous"
	}
	class := profile.ProfileClass(params.Class)
	if class == "" {
		class = profile.ProfileEphemeral
	}
	var session *artemis.Session
	var err error
	if params.ProfileID != "" {
		session, err = s.agent.CreateSessionForProfile(ctx, profile.OpenSessionRequest{
			ProfileID:    profile.ProfileID(params.ProfileID),
			OwnerUserRef: owner,
			Class:        class,
		})
	} else {
		session, err = s.agent.CreateSession(owner)
	}
	if err != nil {
		return s.taskErrorResponse(req, err, ErrBadParams)
	}
	return okResp(req.ID, SessionNewResult{SessionID: session.SessionID(), OwnerUserRef: session.UserID()})
}

func (s *Server) cmdSessionClose(req *Request) *Response {
	var params SessionCloseParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, string(ErrBadParams), err.Error())
		}
	}
	if params.SessionID == "" {
		return errResp(req.ID, string(ErrBadParams), "sessionId required")
	}
	if params.OwnerUserRef == "" {
		return errResp(req.ID, string(ErrBadParams), "ownerUserRef required")
	}
	if err := s.agent.CloseSessionForOwner(params.SessionID, params.OwnerUserRef); err != nil {
		return s.taskErrorResponse(req, err, ErrNoSession)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdSessionList(req *Request) *Response {
	sessions := s.agent.ListSessions()
	result := SessionListResult{Sessions: make([]SessionInfo, 0, len(sessions))}
	for _, session := range sessions {
		result.Sessions = append(result.Sessions, SessionInfo{
			SessionID:    session.SessionID(),
			OwnerUserRef: session.UserID(),
			Active:       session.IsActive(),
			TabCount:     session.TabCount(),
		})
	}
	return okResp(req.ID, result)
}

func (s *Server) cmdPageOpen(ctx context.Context, req *Request) *Response {
	var params PageOpenParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "unknown sessionId")
	}
	pageID, page, err := session.OpenPage(ctx, params.URL, params.RunScripts)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrFetchFailed)
	}
	return okResp(req.ID, PageOpenResult{
		PageID: pageID,
		URL:    page.URL(),
		Status: page.StatusCode(),
		Title:  page.Title(),
	})
}

func (s *Server) cmdPageClose(req *Request) *Response {
	var params PageCloseParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	if err := session.ClosePage(params.PageID); err != nil {
		return s.taskErrorResponse(req, err, ErrNoPage)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageEval(ctx context.Context, req *Request) *Response {
	var params PageEvalParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	v, err := session.Eval(ctx, params.PageID, params.Expr)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrEvalFailed)
	}
	return okResp(req.ID, PageEvalResult{Value: v.String()})
}

func (s *Server) cmdPageDump(req *Request) *Response {
	var params PageDumpParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	if params.Format == "" {
		params.Format = string(DumpMarkdown)
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	data, err := session.Dump(params.PageID, params.Format)
	if err != nil {
		var taskErr *artemis.TaskError
		if errors.As(err, &taskErr) && taskErr.Code == artemis.TaskErrorInvalidInput && strings.Contains(taskErr.Error(), "unknown format") {
			return errResp(req.ID, string(ErrBadFormat), taskErr.Error())
		}
		return s.taskErrorResponse(req, err, ErrBadFormat)
	}
	return okResp(req.ID, PageDumpResult{Data: data})
}

func (s *Server) cmdPageClickByText(ctx context.Context, req *Request) *Response {
	var params PageClickByTextParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	if err := session.ClickByText(ctx, params.PageID, params.Text); err != nil {
		return s.taskErrorResponse(req, err, ErrClickFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageType(req *Request) *Response {
	var params PageTypeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	if err := session.Type(params.PageID, params.Selector, params.Text); err != nil {
		return s.taskErrorResponse(req, err, ErrTypeFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageWaitIdle(ctx context.Context, req *Request) *Response {
	var params PageWaitIdleParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	if err := session.WaitIdle(ctx, params.PageID); err != nil {
		return s.taskErrorResponse(req, err, ErrWaitFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageAssert(ctx context.Context, req *Request) *Response {
	var params PageAssertParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	result, err := session.Assert(ctx, params.PageID, params.Mode, params.Selector, params.Substring, params.Expr, params.Status, params.Want)
	if err != nil {
		var taskErr *artemis.TaskError
		if errors.As(err, &taskErr) && taskErr.Code == artemis.TaskErrorInvalidInput && strings.Contains(taskErr.Error(), "unknown assert mode") {
			return errResp(req.ID, string(ErrBadMode), taskErr.Error())
		}
		return s.taskErrorResponse(req, err, ErrAssertFailed)
	}
	return okResp(req.ID, AssertResult{Pass: result.Pass, Got: result.Got})
}

func (s *Server) cmdChromiumAct(ctx context.Context, req *Request) *Response {
	var params ChromiumActParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	outcome, err := session.ChromiumAct(ctx, params.Request)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrCapabilityUnavailable)
	}
	if !outcome.Success {
		return errResp(req.ID, string(outcome.Failure), outcome.Error)
	}
	return okResp(req.ID, ChromiumActResult{Outcome: outcome})
}

func (s *Server) cmdStream(ctx, connCtx context.Context, c *websocket.Conn, req *Request, wg *sync.WaitGroup) *Response {
	var params StreamParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}

	if params.StreamID != "" {
		return s.resumeStream(connCtx, c, req, params.StreamID, params.ResumeFrom)
	}

	streamID := newStreamID()
	session, ok := s.agent.Session(params.SessionID)
	if !ok {
		return errResp(req.ID, string(ErrNoSession), "")
	}
	outbox := &streamOutbox{reqID: req.ID}
	s.mu.Lock()
	s.outbox[streamID] = outbox
	s.mu.Unlock()

	wg.Add(1)
	go func() {
		defer wg.Done()
		s.streamOpen(connCtx, c, session, outbox, streamID, params)
	}()

	return okResp(req.ID, StreamResult{StreamID: streamID})
}

func (s *Server) resumeStream(ctx context.Context, c *websocket.Conn, req *Request, streamID string, resumeFrom int64) *Response {
	s.mu.Lock()
	outbox, ok := s.outbox[streamID]
	s.mu.Unlock()
	if !ok {
		return errResp(req.ID, string(ErrNotFound), "unknown streamId")
	}
	events := outbox.eventsSince(resumeFrom)
	for _, ev := range events {
		if ev.event != nil {
			s.writeEvent(ctx, c, ev.event)
		} else if ev.terminal != nil {
			terminal := *ev.terminal
			terminal.ID = req.ID
			s.writeResp(ctx, c, &terminal)
		}
	}
	if outbox.isClosed() && (len(events) == 0 || events[len(events)-1].terminal == nil) {
		return okResp(req.ID, StreamResult{StreamID: streamID, Complete: true})
	}
	if outbox.isClosed() {
		return nil
	}
	return okResp(req.ID, StreamResult{StreamID: streamID, Resumed: true})
}

func (s *Server) streamOpen(ctx context.Context, c *websocket.Conn, session *artemis.Session, outbox *streamOutbox, streamID string, params StreamParams) {
	outbox.pushEvent(&Event{Event: "stream.progress", Params: StreamProgress{StreamID: streamID, Stage: "open"}})

	pageID, page, err := session.OpenPage(ctx, params.URL, params.RunScripts)
	if err != nil {
		resp := s.taskErrorResponse(&Request{ID: outbox.reqID}, err, ErrFetchFailed)
		outbox.pushTerminal(resp)
		s.writeResp(ctx, c, resp)
		s.mu.Lock()
		delete(s.outbox, streamID)
		s.mu.Unlock()
		return
	}

	outbox.pushEvent(&Event{Event: "stream.progress", Params: StreamProgress{StreamID: streamID, Stage: "loaded"}})
	outbox.pushEvent(&Event{Event: "stream.result", Params: PageOpenResult{
		PageID: pageID,
		URL:    page.URL(),
		Status: page.StatusCode(),
		Title:  page.Title(),
	}})

	resp := okResp(outbox.reqID, PageOpenResult{
		PageID: pageID,
		URL:    page.URL(),
		Status: page.StatusCode(),
		Title:  page.Title(),
	})
	outbox.pushTerminal(resp)
	s.writeResp(ctx, c, resp)
}

func (s *Server) cmdCancel(req *Request) *Response {
	var params CancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	if s.cancelRequest(params.RequestID) {
		return okResp(req.ID, CancelResult{Cancelled: true})
	}
	return errResp(req.ID, string(ErrNotFound), "request not found")
}

func (s *Server) cmdTokenRotate(req *Request) *Response {
	if s.getAuthToken() == "" {
		return errResp(req.ID, string(ErrCapabilityUnavailable), "token rotation requires an authenticated server")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return errResp(req.ID, string(ErrExecutionFailed), err.Error())
	}
	token := hex.EncodeToString(b)
	s.setAuthToken(token)
	return okResp(req.ID, TokenRotateResult{Token: token})
}

func (o *streamOutbox) pushEvent(ev *Event) uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	seq := o.seq
	o.events = append(o.events, streamEvent{seq: seq, event: ev})
	return seq
}

func (o *streamOutbox) pushTerminal(resp *Response) uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	seq := o.seq
	o.terminal = resp
	o.closed = true
	o.events = append(o.events, streamEvent{seq: seq, terminal: resp})
	return seq
}

func (o *streamOutbox) eventsSince(resumeFrom int64) []streamEvent {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []streamEvent
	for _, ev := range o.events {
		if int64(ev.seq) > resumeFrom {
			out = append(out, ev)
		}
	}
	if o.terminal != nil {
		found := false
		for _, ev := range out {
			if ev.terminal != nil {
				found = true
				break
			}
		}
		if !found && int64(o.seq) > resumeFrom {
			out = append(out, streamEvent{seq: o.seq, terminal: o.terminal})
		}
	}
	return out
}

func (o *streamOutbox) isClosed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.closed
}

func newStreamID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return strconv.FormatUint(uint64(time.Now().UnixNano()), 10)
	}
	return hex.EncodeToString(b)
}

func okResp(id string, value any) *Response {
	return &Response{ID: id, OK: true, Value: value}
}

func errResp(id, code, msg string) *Response {
	return &Response{ID: id, OK: false, Error: &Err{Code: code, Message: msg}}
}

type rateLimiter struct {
	limit  int
	burst  int
	tokens float64
	last   time.Time
	mu     sync.Mutex
}

func newRateLimiter(r RateLimit) *rateLimiter {
	if r.RequestsPerSecond <= 0 {
		return nil
	}
	burst := r.Burst
	if burst <= 0 {
		burst = r.RequestsPerSecond
	}
	if burst < 1 {
		burst = 1
	}
	return &rateLimiter{limit: r.RequestsPerSecond, burst: burst, tokens: float64(burst), last: time.Now()}
}

func (r *rateLimiter) Allow() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.tokens += now.Sub(r.last).Seconds() * float64(r.limit)
	if r.tokens > float64(r.burst) {
		r.tokens = float64(r.burst)
	}
	if r.tokens >= 1 {
		r.tokens--
		r.last = now
		return true
	}
	return false
}
