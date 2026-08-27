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
	// AuthToken is the bearer token required for every connection.
	// An empty token makes the server fail closed.
	AuthToken string
	// OriginPatterns lists allowed WebSocket origins. Empty selects the
	// loopback Omnimus defaults.
	OriginPatterns []string
	// RateLimit configures per-connection and normalized-client limits.
	// Zero fields receive secure defaults.
	RateLimit RateLimit
}

// RateLimit configures per-connection and normalized-client throttling.
type RateLimit struct {
	RequestsPerSecond       int
	Burst                   int
	ClientRequestsPerMinute int
	ClientBurst             int
	ClientBucketTTL         time.Duration
}

// Server is a single Agent WebSocket steering server. Multiple concurrent
// sessions can be active per server.
type Server struct {
	agent            *artemis.Agent
	opts             Opts
	mu               sync.Mutex
	writeMu          sync.Mutex
	nextSeq          atomic.Int64
	srv              *http.Server
	authTokenMu      sync.RWMutex
	authToken        string
	clientSigningKey []byte
	authStateMu      sync.RWMutex
	authGeneration   atomic.Uint64
	inflight         map[requestKey]context.CancelFunc
	outbox           map[string]*streamOutbox
	sessionOwner     map[string]string
	clientLimiter    *clientRateLimiter
	connections      map[*websocket.Conn]uint64
}

type requestKey struct {
	clientID  string
	requestID string
}

// streamOutbox stores ordered events and a terminal response for a stream so
// that a reconnecting client can resume without duplication.
type streamOutbox struct {
	mu       sync.Mutex
	events   []streamEvent
	terminal *Response
	closed   bool
	seq      int64
	reqID    string
	ownerID  string
}

type streamEvent struct {
	seq      int64
	event    *Event
	terminal *Response
}

// New creates a Server bound to an Agent. The returned Server must be Closed
// after Serve returns.
func New(agent *artemis.Agent, opts Opts) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	acceptOptions := websocket.AcceptOptions{}
	if opts.AcceptOptions != nil {
		acceptOptions = *opts.AcceptOptions
	}
	if len(opts.OriginPatterns) == 0 {
		opts.OriginPatterns = append([]string(nil), defaultOriginPatterns...)
	} else if err := ValidateOriginPatterns(opts.OriginPatterns); err != nil {
		opts.Logger.Warn("invalid origin allowlist; using secure defaults", "err", err)
		opts.OriginPatterns = append([]string(nil), defaultOriginPatterns...)
	}
	acceptOptions.OriginPatterns = append([]string(nil), opts.OriginPatterns...)
	acceptOptions.InsecureSkipVerify = false
	opts.AcceptOptions = &acceptOptions
	opts.RateLimit = normalizeRateLimit(opts.RateLimit)
	s := &Server{
		agent:            agent,
		opts:             opts,
		authToken:        opts.AuthToken,
		clientSigningKey: []byte(rand.Text()),
		inflight:         make(map[requestKey]context.CancelFunc),
		outbox:           make(map[string]*streamOutbox),
		sessionOwner:     make(map[string]string),
		clientLimiter:    newClientRateLimiter(opts.RateLimit),
		connections:      make(map[*websocket.Conn]uint64),
	}
	s.authGeneration.Store(1)
	return s
}

// ListenAndServe blocks while serving on addr until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	if s.getAuthToken() == "" {
		return fmt.Errorf("serve authentication token required")
	}
	if err := validateLoopbackAddress(addr); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("ok")); err != nil {
			s.opts.Logger.Warn("health response write", "err", err)
		}
	})
	s.srv = &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			s.opts.Logger.Warn("server shutdown", "err", err)
		}
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
	if hasQueryCredential(r) {
		http.Error(w, "credentials are not accepted in the URL", http.StatusBadRequest)
		return
	}
	if !validateRequestHost(r) {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	rateKey := normalizeClientAddress(r.RemoteAddr)
	if !s.clientLimiter.allow(rateKey) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	authGeneration, authenticated := s.authenticateRequestToken(r.Header.Get("Authorization"))
	if !authenticated {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	clientID := strings.TrimSpace(r.Header.Get(ClientIDHeader))
	if clientID == "" {
		var err error
		clientID, err = issueClientID(s.clientSigningKey)
		if err != nil {
			s.opts.Logger.Error("issue client identity", "err", err)
			http.Error(w, "client identity unavailable", http.StatusInternalServerError)
			return
		}
	} else if !validateClientID(clientID, s.clientSigningKey) {
		http.Error(w, "invalid client identity", http.StatusUnauthorized)
		return
	}
	w.Header().Set(ClientIDHeader, clientID)

	c, err := websocket.Accept(w, r, s.opts.AcceptOptions)
	if err != nil {
		s.opts.Logger.Warn("ws accept failed", "err", err)
		return
	}
	defer func() {
		if closeErr := c.CloseNow(); closeErr != nil {
			s.opts.Logger.Warn("ws close", "err", closeErr)
		}
	}()
	s.trackConnection(c, authGeneration, true)
	defer s.trackConnection(c, authGeneration, false)
	// Page dumps (especially HTML) can easily exceed the default 32KB
	// read limit. Allow up to 8MB per message.
	c.SetReadLimit(8 << 20)

	ctx := r.Context()
	var wg sync.WaitGroup
	client := clientIdentity{id: clientID, ownerRef: clientOwnerRef(clientID), rateKey: rateKey, authGeneration: authGeneration}
	connectionLimiter := newTokenBucket(s.opts.RateLimit.RequestsPerSecond, time.Second, s.opts.RateLimit.Burst, time.Now())
	consecutiveRateHits := 0

	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			break
		}
		if !connectionLimiter.allow(time.Now()) || !s.clientLimiter.allow(client.rateKey) {
			consecutiveRateHits++
			s.writeResp(ctx, c, errResp("", string(ErrRateExceeded), "rate limit exceeded"))
			if consecutiveRateHits >= 10 {
				if closeErr := c.Close(websocket.StatusPolicyViolation, "rate limit exceeded"); closeErr != nil {
					s.opts.Logger.Warn("close rate-limited websocket", "err", closeErr)
				}
				break
			}
			continue
		}
		consecutiveRateHits = 0
		var req Request
		if err := json.Unmarshal(data, &req); err != nil {
			s.writeResp(ctx, c, &Response{
				ID: req.ID, OK: false,
				Error: &Err{Code: string(ErrBadRequest), Message: err.Error()},
			})
			continue
		}
		wg.Add(1)
		go s.handleRequest(ctx, c, client, &req, &wg)
	}
	wg.Wait()
}

func (s *Server) handleRequest(ctx context.Context, c *websocket.Conn, client clientIdentity, req *Request, wg *sync.WaitGroup) {
	defer wg.Done()
	if verr := s.checkVersion(req); verr != nil {
		s.writeResp(ctx, c, verr)
		return
	}
	dispatchCtx, cancel := context.WithCancel(ctx)
	s.registerInflight(client.id, req.ID, cancel)
	defer func() {
		cancel()
		s.unregisterInflight(client.id, req.ID)
	}()
	if req.Cmd == string(CmdTokenRotate) {
		s.cancelOtherInflight(client.id, req.ID)
		s.authStateMu.Lock()
		defer s.authStateMu.Unlock()
	} else {
		s.authStateMu.RLock()
		defer s.authStateMu.RUnlock()
	}
	if client.authGeneration != s.authGeneration.Load() {
		s.writeResp(ctx, c, errResp(req.ID, string(ErrOwnershipDenied), "authentication generation expired"))
		return
	}
	resp := s.dispatch(dispatchCtx, ctx, c, client, req, wg)
	if resp != nil {
		s.writeResp(ctx, c, resp)
	}
	if req.Cmd == string(CmdTokenRotate) && resp != nil && resp.OK {
		s.invalidateAuthenticatedState()
	}
}

func (s *Server) writeResp(ctx context.Context, c *websocket.Conn, r *Response) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	r.Version = ProtocolVersion
	r.Seq = s.nextSeq.Add(1)
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
	ev.Seq = s.nextSeq.Add(1)
	out, err := json.Marshal(ev)
	if err != nil {
		s.opts.Logger.Error("marshal event", "err", err)
		return
	}
	if err := c.Write(ctx, websocket.MessageText, out); err != nil {
		s.opts.Logger.Warn("ws event", "err", err)
	}
}

func (s *Server) dispatch(ctx, connCtx context.Context, c *websocket.Conn, client clientIdentity, req *Request, wg *sync.WaitGroup) *Response {
	switch req.Cmd {
	case string(CmdVersion):
		return s.cmdVersion(req)
	case string(CmdCapabilities):
		return s.cmdCapabilities(req)
	case string(CmdHeartbeat):
		return s.cmdHeartbeat(req)
	case string(CmdSessionNew):
		return s.cmdSessionNew(ctx, client, req)
	case string(CmdSessionClose):
		return s.cmdSessionClose(client, req)
	case string(CmdSessionList):
		return s.cmdSessionList(client, req)
	case string(CmdPageOpen):
		return s.cmdPageOpen(ctx, client, req)
	case string(CmdPageClose):
		return s.cmdPageClose(client, req)
	case string(CmdPageEval):
		return s.cmdPageEval(ctx, client, req)
	case string(CmdPageDump):
		return s.cmdPageDump(client, req)
	case string(CmdPageClickByText):
		return s.cmdPageClickByText(ctx, client, req)
	case string(CmdPageType):
		return s.cmdPageType(client, req)
	case string(CmdPageWaitIdle):
		return s.cmdPageWaitIdle(ctx, client, req)
	case string(CmdPageAssert):
		return s.cmdPageAssert(ctx, client, req)
	case string(CmdChromiumAct):
		return s.cmdChromiumAct(ctx, client, req)
	case string(CmdStream):
		return s.cmdStream(ctx, connCtx, c, client, req, wg)
	case string(CmdCancel):
		return s.cmdCancel(client, req)
	case string(CmdTokenRotate):
		return s.cmdTokenRotate(req)
	default:
		return errResp(req.ID, string(ErrUnknownCmd), fmt.Sprintf("unknown cmd %q", req.Cmd))
	}
}

func (s *Server) registerInflight(clientID, id string, cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight[requestKey{clientID: clientID, requestID: id}] = cancel
}

func (s *Server) unregisterInflight(clientID, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.inflight, requestKey{clientID: clientID, requestID: id})
}

func (s *Server) cancelRequest(clientID, id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn, ok := s.inflight[requestKey{clientID: clientID, requestID: id}]
	if ok && fn != nil {
		fn()
	}
	return ok
}

func (s *Server) cancelOtherInflight(clientID, requestID string) {
	current := requestKey{clientID: clientID, requestID: requestID}
	s.mu.Lock()
	cancellations := make([]context.CancelFunc, 0, len(s.inflight))
	for key, cancel := range s.inflight {
		if key != current && cancel != nil {
			cancellations = append(cancellations, cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancellations {
		cancel()
	}
}

func (s *Server) trackConnection(connection *websocket.Conn, generation uint64, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.connections[connection] = generation
		return
	}
	delete(s.connections, connection)
}

func (s *Server) invalidateAuthenticatedState() {
	s.mu.Lock()
	connections := make([]*websocket.Conn, 0, len(s.connections))
	currentGeneration := s.authGeneration.Load()
	for connection, generation := range s.connections {
		if generation != currentGeneration {
			connections = append(connections, connection)
		}
	}
	for _, cancel := range s.inflight {
		cancel()
	}
	s.inflight = make(map[requestKey]context.CancelFunc)
	s.outbox = make(map[string]*streamOutbox)
	sessionIDs := make([]string, 0, len(s.sessionOwner))
	for sessionID := range s.sessionOwner {
		sessionIDs = append(sessionIDs, sessionID)
	}
	s.sessionOwner = make(map[string]string)
	s.mu.Unlock()
	for _, sessionID := range sessionIDs {
		if err := s.agent.CloseSession(sessionID); err != nil {
			s.opts.Logger.Warn("close session after token rotation", "session_id", sessionID, "err", err)
		}
	}
	for _, connection := range connections {
		if err := connection.CloseNow(); err != nil {
			s.opts.Logger.Warn("close stale websocket after token rotation", "err", err)
		}
	}
}

func (s *Server) getAuthToken() string {
	s.authTokenMu.RLock()
	defer s.authTokenMu.RUnlock()
	return s.authToken
}

func (s *Server) authenticateRequestToken(header string) (uint64, bool) {
	s.authTokenMu.RLock()
	defer s.authTokenMu.RUnlock()
	return s.authGeneration.Load(), authenticateBearer(header, s.authToken)
}

func (s *Server) rotateAuthToken(token string) {
	s.authTokenMu.Lock()
	defer s.authTokenMu.Unlock()
	s.authToken = token
	s.authGeneration.Add(1)
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

func (s *Server) ownedSession(clientID, sessionID string) (*artemis.Session, *Response) {
	session, ok := s.agent.Session(sessionID)
	if !ok {
		s.forgetSession(sessionID)
		return nil, errResp("", string(ErrNoSession), "unknown sessionId")
	}
	s.mu.Lock()
	ownerID, tracked := s.sessionOwner[sessionID]
	s.mu.Unlock()
	if !tracked {
		return nil, errResp("", string(ErrNoSession), "unknown sessionId")
	}
	if ownerID != clientID {
		return nil, errResp("", string(ErrOwnershipDenied), "session ownership denied")
	}
	return session, nil
}

func (s *Server) trackSession(sessionID, clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionOwner[sessionID] = clientID
}

func (s *Server) forgetSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessionOwner, sessionID)
}

func sessionError(req *Request, response *Response) *Response {
	response.ID = req.ID
	return response
}

func (s *Server) cmdSessionNew(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params SessionNewParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, string(ErrBadParams), err.Error())
		}
	}
	if params.OwnerUserRef != "" && params.OwnerUserRef != client.ownerRef {
		return errResp(req.ID, string(ErrOwnershipDenied), "ownerUserRef is assigned by the authenticated server")
	}
	owner := client.ownerRef
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
	s.trackSession(session.SessionID(), client.id)
	return okResp(req.ID, SessionNewResult{SessionID: session.SessionID(), OwnerUserRef: session.UserID()})
}

func (s *Server) cmdSessionClose(client clientIdentity, req *Request) *Response {
	var params SessionCloseParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResp(req.ID, string(ErrBadParams), err.Error())
		}
	}
	if params.SessionID == "" {
		return errResp(req.ID, string(ErrBadParams), "sessionId required")
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	if params.OwnerUserRef != "" && params.OwnerUserRef != session.UserID() {
		return errResp(req.ID, string(ErrOwnershipDenied), "ownerUserRef does not match authenticated client")
	}
	if err := s.agent.CloseSessionForOwner(params.SessionID, session.UserID()); err != nil {
		return s.taskErrorResponse(req, err, ErrNoSession)
	}
	s.forgetSession(params.SessionID)
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdSessionList(client clientIdentity, req *Request) *Response {
	sessions := s.agent.ListSessions()
	result := SessionListResult{Sessions: make([]SessionInfo, 0, len(sessions))}
	for _, session := range sessions {
		s.mu.Lock()
		ownerID := s.sessionOwner[session.SessionID()]
		s.mu.Unlock()
		if ownerID != client.id {
			continue
		}
		result.Sessions = append(result.Sessions, SessionInfo{
			SessionID:    session.SessionID(),
			OwnerUserRef: session.UserID(),
			Active:       session.IsActive(),
			TabCount:     session.TabCount(),
		})
	}
	return okResp(req.ID, result)
}

func (s *Server) cmdPageOpen(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageOpenParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
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

func (s *Server) cmdPageClose(client clientIdentity, req *Request) *Response {
	var params PageCloseParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	if err := session.ClosePage(params.PageID); err != nil {
		return s.taskErrorResponse(req, err, ErrNoPage)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageEval(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageEvalParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	v, err := session.Eval(ctx, params.PageID, params.Expr)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrEvalFailed)
	}
	return okResp(req.ID, PageEvalResult{Value: v.String()})
}

func (s *Server) cmdPageDump(client clientIdentity, req *Request) *Response {
	var params PageDumpParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	if params.Format == "" {
		params.Format = string(DumpMarkdown)
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
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

func (s *Server) cmdPageClickByText(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageClickByTextParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	if err := session.ClickByText(ctx, params.PageID, params.Text); err != nil {
		return s.taskErrorResponse(req, err, ErrClickFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageType(client clientIdentity, req *Request) *Response {
	var params PageTypeParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	if err := session.Type(params.PageID, params.Selector, params.Text); err != nil {
		return s.taskErrorResponse(req, err, ErrTypeFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageWaitIdle(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageWaitIdleParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	if err := session.WaitIdle(ctx, params.PageID); err != nil {
		return s.taskErrorResponse(req, err, ErrWaitFailed)
	}
	return okResp(req.ID, EmptyResult{})
}

func (s *Server) cmdPageAssert(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageAssertParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
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

func (s *Server) cmdChromiumAct(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumActParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
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

func (s *Server) cmdStream(ctx, connCtx context.Context, c *websocket.Conn, client clientIdentity, req *Request, wg *sync.WaitGroup) *Response {
	var params StreamParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}

	if params.StreamID != "" {
		return s.resumeStream(connCtx, c, client, req, params.StreamID, params.ResumeFrom)
	}

	streamID := newStreamID()
	session, sessionErr := s.ownedSession(client.id, params.SessionID)
	if sessionErr != nil {
		return sessionError(req, sessionErr)
	}
	outbox := &streamOutbox{reqID: req.ID, ownerID: client.id}
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

func (s *Server) resumeStream(ctx context.Context, c *websocket.Conn, client clientIdentity, req *Request, streamID string, resumeFrom int64) *Response {
	s.mu.Lock()
	outbox, ok := s.outbox[streamID]
	s.mu.Unlock()
	if !ok {
		return errResp(req.ID, string(ErrNotFound), "unknown streamId")
	}
	if outbox.ownerID != client.id {
		return errResp(req.ID, string(ErrOwnershipDenied), "stream ownership denied")
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

func (s *Server) cmdCancel(client clientIdentity, req *Request) *Response {
	var params CancelParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	if s.cancelRequest(client.id, params.RequestID) {
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
	s.rotateAuthToken(token)
	return okResp(req.ID, TokenRotateResult{Token: token})
}

func (o *streamOutbox) pushEvent(ev *Event) int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	seq := o.seq
	o.events = append(o.events, streamEvent{seq: seq, event: ev})
	return seq
}

func (o *streamOutbox) pushTerminal(resp *Response) int64 {
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
		if ev.seq > resumeFrom {
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
		if !found && o.seq > resumeFrom {
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
