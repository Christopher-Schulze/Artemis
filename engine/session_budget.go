package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Christopher-Schulze/Artemis/network"
)

const (
	DefaultSessionMaxTabs          = 8
	DefaultSessionMaxRequests      = 256
	DefaultSessionMaxResponseBytes = int64(256 * 1024 * 1024)
	DefaultSessionMaxConcurrency   = 16
	DefaultSessionMaxMemoryBytes   = int64(512 * 1024 * 1024)
	DefaultSessionTimeout          = 30 * time.Minute
)

// SessionBudget contains hard renderless-engine limits. Zero values receive
// secure defaults; negative values are invalid.
type SessionBudget struct {
	MaxTabs          int
	MaxRequests      int
	MaxResponseBytes int64
	MaxMemoryBytes   int64
	MaxDiskBytes     int64
	MaxConcurrency   int
	Timeout          time.Duration
}

// AdmissionLimits are live engine-wide admission ceilings supplied by the
// embedding application's resource authority. A positive field tightens the
// matching static SessionBudget limit at each admission; zero leaves it.
type AdmissionLimits struct {
	MaxTabs        int
	MaxConcurrency int
}

// SessionUsage is an atomic snapshot of admitted session work.
type SessionUsage struct {
	Requests      int
	ResponseBytes int64
	MemoryBytes   int64 // Reserved transient input/image capacity, including page rounding; not RSS.
	DiskBytes     int64
	ActiveTabs    int
	Concurrent    int
	Cancelled     bool
	TerminalError string
}

// BudgetResource identifies the exhausted session resource.
type BudgetResource string

const (
	BudgetTabs          BudgetResource = "tabs"
	BudgetRequests      BudgetResource = "requests"
	BudgetResponseBytes BudgetResource = "response_bytes"
	BudgetMemoryBytes   BudgetResource = "memory_bytes"
	BudgetConcurrency   BudgetResource = "concurrency"
	BudgetTimeout       BudgetResource = "timeout"
)

// BudgetError is returned when admitting or accounting work would exceed a
// hard limit. Requests, response bytes and lifetime are accounted per
// network session (network.WithSessionID): the first breach cancels all
// remaining work of that session only. Tabs, concurrency and transient memory
// are engine-wide admission limits: a breach refuses the new work while
// admitted sessions keep running.
type BudgetError struct {
	Resource BudgetResource
	Limit    int64
	Observed int64
}

func (e *BudgetError) Error() string {
	return fmt.Sprintf("artemis session budget exceeded: %s limit=%d observed=%d", e.Resource, e.Limit, e.Observed)
}

// IsBudgetExceeded reports whether err contains a session budget failure.
func IsBudgetExceeded(err error) bool {
	var budgetErr *BudgetError
	return errors.As(err, &budgetErr)
}

func (b *SessionBudget) applyDefaults(defaultDiskBytes int64) {
	if b.MaxTabs == 0 {
		b.MaxTabs = DefaultSessionMaxTabs
	}
	if b.MaxRequests == 0 {
		b.MaxRequests = DefaultSessionMaxRequests
	}
	if b.MaxResponseBytes == 0 {
		b.MaxResponseBytes = DefaultSessionMaxResponseBytes
	}
	if b.MaxMemoryBytes == 0 {
		b.MaxMemoryBytes = DefaultSessionMaxMemoryBytes
	}
	if b.MaxDiskBytes == 0 {
		b.MaxDiskBytes = defaultDiskBytes
	}
	if b.MaxConcurrency == 0 {
		b.MaxConcurrency = DefaultSessionMaxConcurrency
	}
	if b.Timeout == 0 {
		b.Timeout = DefaultSessionTimeout
	}
}

func (b SessionBudget) validate() error {
	if b.MaxTabs < 1 || b.MaxRequests < 1 || b.MaxResponseBytes < 1 || b.MaxMemoryBytes < 1 || b.MaxDiskBytes < 1 || b.MaxConcurrency < 1 || b.Timeout < time.Millisecond {
		return errors.New("engine: session budget limits must be positive and timeout must be at least 1ms")
	}
	return nil
}

type sessionBudgetController struct {
	sharedMemory     network.MemoryAdmission
	liveLimits       func() AdmissionLimits
	mu               sync.Mutex
	limits           SessionBudget
	ctx              context.Context
	cancel           context.CancelCauseFunc
	requests         int
	responseBytes    int64
	memoryBytes      int64
	diskBytes        int64
	diskBySession    map[string]int64
	activeTabs       int
	concurrent       int
	terminal         error
	accounts         map[string]*sessionAccount
	defaultSessionID string
	usageSink        func(string, SessionUsage) error
}

// sessionAccount is the consumption budget of one network session.
type sessionAccount struct {
	requests      int
	responseBytes int64
	tabs          int
	terminal      error
	ctx           context.Context
	cancel        context.CancelCauseFunc
	stopTimer     func() bool
}

func newSessionBudgetController(limits SessionBudget, defaultSessionID string, usageSink func(string, SessionUsage) error) *sessionBudgetController {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &sessionBudgetController{limits: limits, ctx: ctx, cancel: cancel, defaultSessionID: defaultSessionID, usageSink: usageSink, accounts: map[string]*sessionAccount{}}
}

// accountLocked returns the session's account, starting its lifetime budget
// on first use. c.mu must be held.
func (c *sessionBudgetController) accountLocked(sessionID string) *sessionAccount {
	if account := c.accounts[sessionID]; account != nil {
		return account
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	account := &sessionAccount{ctx: ctx, cancel: cancel}
	timer := time.AfterFunc(c.limits.Timeout, func() {
		c.failSession(sessionID, &BudgetError{Resource: BudgetTimeout, Limit: int64(c.limits.Timeout), Observed: int64(c.limits.Timeout) + 1})
	})
	account.stopTimer = timer.Stop
	c.accounts[sessionID] = account
	return account
}

// admissionLimits resolves the effective tab and concurrency ceilings. The
// live authority runs outside c.mu because it may consult other locks.
func (c *sessionBudgetController) admissionLimits() (maxTabs, maxConcurrency int) {
	maxTabs, maxConcurrency = c.limits.MaxTabs, c.limits.MaxConcurrency
	if c.liveLimits == nil {
		return maxTabs, maxConcurrency
	}
	live := c.liveLimits()
	if live.MaxTabs > 0 && live.MaxTabs < maxTabs {
		maxTabs = live.MaxTabs
	}
	if live.MaxConcurrency > 0 && live.MaxConcurrency < maxConcurrency {
		maxConcurrency = live.MaxConcurrency
	}
	return maxTabs, maxConcurrency
}

func (c *sessionBudgetController) sessionOf(ctx context.Context) string {
	return network.SessionID(ctx, c.defaultSessionID)
}

func (c *sessionBudgetController) BeginRequest(ctx context.Context) (context.Context, func(int64) error, error) {
	if ctx == nil {
		return nil, nil, errors.New("engine: request context required")
	}
	sessionID := c.sessionOf(ctx)
	_, maxConcurrency := c.admissionLimits()
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return nil, nil, err
	}
	account := c.accountLocked(sessionID)
	if account.terminal != nil {
		err := account.terminal
		c.mu.Unlock()
		return nil, nil, err
	}
	if account.requests >= c.limits.MaxRequests {
		err := &BudgetError{Resource: BudgetRequests, Limit: int64(c.limits.MaxRequests), Observed: int64(account.requests + 1)}
		c.mu.Unlock()
		c.failSession(sessionID, err)
		return nil, nil, err
	}
	if c.concurrent >= maxConcurrency {
		err := &BudgetError{Resource: BudgetConcurrency, Limit: int64(maxConcurrency), Observed: int64(c.concurrent + 1)}
		c.mu.Unlock()
		return nil, nil, errors.Join(err, c.recordUsage(sessionID))
	}
	account.requests++
	c.requests++
	c.concurrent++
	c.mu.Unlock()
	requestCtx, releaseContext := c.mergeSessionContext(ctx, sessionID)
	var once sync.Once
	finish := func(responseBytes int64) error {
		var result error
		once.Do(func() {
			releaseContext()
			result = c.finishRequest(responseBytes, sessionID)
		})
		return result
	}
	return requestCtx, finish, nil
}

func (c *sessionBudgetController) finishRequest(responseBytes int64, sessionID string) error {
	if responseBytes < 0 {
		responseBytes = 0
	}
	c.mu.Lock()
	if c.concurrent > 0 {
		c.concurrent--
	}
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return err
	}
	account := c.accountLocked(sessionID)
	if account.terminal != nil {
		err := account.terminal
		c.mu.Unlock()
		return err
	}
	observed := account.responseBytes + responseBytes
	if observed > c.limits.MaxResponseBytes {
		err := &BudgetError{Resource: BudgetResponseBytes, Limit: c.limits.MaxResponseBytes, Observed: observed}
		c.mu.Unlock()
		c.failSession(sessionID, err)
		return err
	}
	account.responseBytes = observed
	c.responseBytes += responseBytes
	c.mu.Unlock()
	if err := c.recordUsage(sessionID); err != nil {
		c.fail(err)
		return err
	}
	return nil
}

func (c *sessionBudgetController) admitSynthetic(ctx context.Context, responseBytes int64) error {
	_, finish, err := c.BeginRequest(ctx)
	if err != nil {
		return err
	}
	return finish(responseBytes)
}

func (c *sessionBudgetController) acquireTab(sessionID string) error {
	if sessionID == "" {
		sessionID = c.defaultSessionID
	}
	maxTabs, _ := c.admissionLimits()
	c.mu.Lock()
	if c.terminal != nil {
		err := c.terminal
		c.mu.Unlock()
		return err
	}
	account := c.accountLocked(sessionID)
	if account.terminal != nil {
		err := account.terminal
		c.mu.Unlock()
		return err
	}
	if c.activeTabs >= maxTabs {
		// The tab cap is admission control: the request is refused, while
		// pages and sessions already admitted keep running.
		err := &BudgetError{Resource: BudgetTabs, Limit: int64(maxTabs), Observed: int64(c.activeTabs + 1)}
		c.mu.Unlock()
		return errors.Join(err, c.recordUsage(sessionID))
	}
	c.activeTabs++
	account.tabs++
	c.mu.Unlock()
	if err := c.recordUsage(sessionID); err != nil {
		c.mu.Lock()
		c.activeTabs--
		account.tabs--
		c.mu.Unlock()
		c.fail(err)
		return err
	}
	return nil
}

func (c *sessionBudgetController) releaseTab(sessionID string) error {
	if sessionID == "" {
		sessionID = c.defaultSessionID
	}
	c.mu.Lock()
	if c.activeTabs > 0 {
		c.activeTabs--
	}
	if account := c.accounts[sessionID]; account != nil && account.tabs > 0 {
		account.tabs--
	}
	c.mu.Unlock()
	if err := c.recordUsage(sessionID); err != nil {
		c.fail(err)
		return err
	}
	return nil
}

func (c *sessionBudgetController) setDiskUsage(sessionID string, bytes int64) error {
	if bytes < 0 {
		return errors.New("engine: disk usage cannot be negative")
	}
	if sessionID == "" {
		sessionID = c.defaultSessionID
	}
	c.mu.Lock()
	if c.diskBySession == nil {
		c.diskBySession = make(map[string]int64)
	}
	previous, existed := c.diskBySession[sessionID]
	c.diskBySession[sessionID] = bytes
	c.diskBytes += bytes - previous
	c.mu.Unlock()
	if err := c.recordUsage(sessionID); err != nil {
		c.mu.Lock()
		c.diskBytes += previous - bytes
		if existed {
			c.diskBySession[sessionID] = previous
		} else {
			delete(c.diskBySession, sessionID)
		}
		c.mu.Unlock()
		c.fail(err)
		return err
	}
	return nil
}

// fail ends all engine work (engine close or failed accounting authority).
func (c *sessionBudgetController) fail(err error) {
	c.mu.Lock()
	if c.terminal == nil {
		c.terminal = err
	}
	terminal := c.terminal
	accounts := make([]*sessionAccount, 0, len(c.accounts))
	for _, account := range c.accounts {
		accounts = append(accounts, account)
	}
	c.mu.Unlock()
	c.cancel(terminal)
	for _, account := range accounts {
		account.stopTimer()
	}
}

// failSession ends the remaining work of one session after its budget
// breach; other sessions keep running.
func (c *sessionBudgetController) failSession(sessionID string, err error) {
	c.mu.Lock()
	account := c.accountLocked(sessionID)
	if account.terminal == nil {
		account.terminal = err
	}
	terminal := account.terminal
	c.mu.Unlock()
	account.cancel(terminal)
	account.stopTimer()
	if recordErr := c.recordUsage(sessionID); recordErr != nil {
		c.fail(recordErr)
	}
}

// releaseSession forgets a closed session's account and stops its lifetime
// timer; a later request with the same ID starts a fresh budget.
func (c *sessionBudgetController) releaseSession(sessionID string) {
	c.mu.Lock()
	account := c.accounts[sessionID]
	delete(c.accounts, sessionID)
	c.diskBytes -= c.diskBySession[sessionID]
	delete(c.diskBySession, sessionID)
	c.mu.Unlock()
	if account != nil {
		account.stopTimer()
		account.cancel(context.Canceled)
	}
}

func (c *sessionBudgetController) mergeContext(parent context.Context) (context.Context, context.CancelFunc) {
	return c.mergeSessionContext(parent, c.sessionOf(parent))
}

// mergeSessionContext binds parent to the engine lifetime and the session's
// budget lifetime.
func (c *sessionBudgetController) mergeSessionContext(parent context.Context, sessionID string) (context.Context, context.CancelFunc) {
	c.mu.Lock()
	account := c.accountLocked(sessionID)
	c.mu.Unlock()
	ctx, cancel := context.WithCancelCause(parent)
	stopSession := context.AfterFunc(account.ctx, func() { cancel(context.Cause(account.ctx)) })
	stop := context.AfterFunc(c.ctx, func() { cancel(context.Cause(c.ctx)) })
	return ctx, func() {
		stop()
		stopSession()
		cancel(context.Canceled)
	}
}

func (c *sessionBudgetController) err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.terminal
}

// sessionErr returns the engine's or the session's terminal budget error.
func (c *sessionBudgetController) sessionErr(sessionID string) error {
	if sessionID == "" {
		sessionID = c.defaultSessionID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminal != nil {
		return c.terminal
	}
	if account := c.accounts[sessionID]; account != nil {
		return account.terminal
	}
	return nil
}

// usage is the engine-wide aggregate across all sessions.
func (c *sessionBudgetController) usage() SessionUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	usage := SessionUsage{
		Requests: c.requests, ResponseBytes: c.responseBytes, DiskBytes: c.diskBytes, ActiveTabs: c.activeTabs,
		Concurrent: c.concurrent, MemoryBytes: c.memoryBytes, Cancelled: c.terminal != nil,
	}
	if c.terminal != nil {
		usage.TerminalError = c.terminal.Error()
	}
	return usage
}

// sessionUsage is one session's consumption plus the engine-wide admission
// state it shares.
func (c *sessionBudgetController) sessionUsage(sessionID string) SessionUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	usage := SessionUsage{Concurrent: c.concurrent, MemoryBytes: c.memoryBytes, DiskBytes: c.diskBySession[sessionID], Cancelled: c.terminal != nil}
	terminal := c.terminal
	if account := c.accounts[sessionID]; account != nil {
		usage.Requests, usage.ResponseBytes, usage.ActiveTabs = account.requests, account.responseBytes, account.tabs
		if terminal == nil {
			terminal = account.terminal
		}
	}
	if terminal != nil {
		usage.Cancelled, usage.TerminalError = true, terminal.Error()
	}
	return usage
}

func (c *sessionBudgetController) recordUsage(sessionID string) error {
	if c.usageSink == nil {
		return nil
	}
	if sessionID == "" {
		sessionID = c.defaultSessionID
	}
	if err := c.usageSink(sessionID, c.sessionUsage(sessionID)); err != nil {
		return fmt.Errorf("engine: record resource diagnostics: %w", err)
	}
	return nil
}

func (c *sessionBudgetController) close() error {
	c.fail(context.Canceled)
	return c.recordUsage(c.defaultSessionID)
}
