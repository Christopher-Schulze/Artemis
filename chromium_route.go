package artemis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/Christopher-Schulze/Artemis/bridge"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
	"github.com/Christopher-Schulze/Artemis/stealth"
)

type ownedChromiumExecutor struct{ agent *Agent }

func (e ownedChromiumExecutor) Execute(ctx context.Context, request artemisrouter.ExecutionRequest) (artemisrouter.ExecutionOutput, error) {
	session, ok := e.agent.Session(request.State.SessionID)
	if !ok {
		return artemisrouter.ExecutionOutput{}, newTaskError(TaskErrorSessionNotFound, "chromium_route", fmt.Errorf("session not active"))
	}
	if request.Mode == artemisrouter.ModeStealth {
		_, requested, err := stealth.PolicyFromEnv()
		if err != nil || !requested {
			return artemisrouter.ExecutionOutput{}, &artemisrouter.RouteFailure{Reason: "policy_denied", Cause: errors.Join(fmt.Errorf("stealth requires explicit acknowledged configuration"), err)}
		}
	}
	if request.Method != "" && request.Method != http.MethodGet || len(request.Body) > 0 || len(request.Headers) > 0 {
		return artemisrouter.ExecutionOutput{}, &artemisrouter.RouteFailure{Reason: "chromium_request_unavailable", Cause: fmt.Errorf("CDP escalation cannot replay custom methods, bodies or headers")}
	}
	execCtx, done, taskErr := session.chromiumOperation(ctx)
	if taskErr != nil {
		return artemisrouter.ExecutionOutput{}, taskErr
	}
	defer done()
	if err := session.AddTab(); err != nil {
		return artemisrouter.ExecutionOutput{}, err
	}
	session.chromiumMu.Lock()
	hasContext := session.chromiumContext != nil
	session.chromiumMu.Unlock()
	if !hasContext {
		state, err := e.sessionTransferState(session, request)
		if err != nil {
			return artemisrouter.ExecutionOutput{}, errors.Join(err, session.releaseChromiumTab())
		}
		request.State = state
	}
	page, err := session.newChromiumPage(execCtx, request.URL, request.State)
	if err != nil {
		return artemisrouter.ExecutionOutput{}, errors.Join(err, session.releaseChromiumTab())
	}
	output, err := (artemisrouter.ChromiumExecutor{Page: page.page, Observer: page.observer}).Read(execCtx, request.State)
	if err != nil {
		return artemisrouter.ExecutionOutput{}, errors.Join(err, page.close(), session.releaseChromiumTab())
	}
	output.Resource = &chromiumRouteResource{page: page, session: session}
	return output, nil
}

func (e ownedChromiumExecutor) sessionTransferState(session *Session, request artemisrouter.ExecutionRequest) (artemisrouter.BrowserState, error) {
	state := request.State.Clone()
	e.agent.mu.RLock()
	runtime, ok := e.agent.runtime.(*ownedEngineRuntime)
	e.agent.mu.RUnlock()
	if !ok {
		return state, nil
	}
	parsed, err := url.Parse(request.URL)
	if err != nil {
		return state, err
	}
	jar, err := runtime.HTTPClient().CookieJarForSession(session.id)
	if err != nil {
		return state, err
	}
	if state.Cookies == nil {
		state.Cookies = jar.Cookies(parsed)
	}
	if state.LocalStorage == nil {
		state.LocalStorage = runtime.SessionLocalStorageSnapshot(session.id, request.URL)
	}
	if state.URL == "" {
		state.URL = request.URL
	}
	return state, nil
}

type chromiumRouteResource struct {
	page    *ownedChromiumPage
	session *Session
}

func (r *chromiumRouteResource) Close() error {
	if r.page == nil {
		return nil
	}
	page := r.page
	r.page = nil
	return errors.Join(page.close(), r.session.releaseChromiumTab())
}

type chromiumCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	URL      string `json:"url"`
	Path     string `json:"path,omitempty"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite,omitempty"`
}

func transferCookies(ctx context.Context, page *bridge.Page, targetURL string, state artemisrouter.BrowserState) error {
	if len(state.Cookies) == 0 {
		return nil
	}
	if state.URL != "" && !sameTransferOrigin(state.URL, targetURL) {
		return fmt.Errorf("cookie state transfer requires the original origin")
	}
	cookies := make([]chromiumCookie, 0, len(state.Cookies))
	for _, cookie := range state.Cookies {
		if cookie == nil {
			continue
		}
		sameSite := ""
		switch cookie.SameSite {
		case http.SameSiteLaxMode:
			sameSite = "Lax"
		case http.SameSiteStrictMode:
			sameSite = "Strict"
		case http.SameSiteNoneMode:
			sameSite = "None"
		}
		cookies = append(cookies, chromiumCookie{Name: cookie.Name, Value: cookie.Value, URL: targetURL, Path: cookie.Path, Secure: cookie.Secure, HTTPOnly: cookie.HttpOnly, SameSite: sameSite})
	}
	return page.Call(ctx, "Network.setCookies", struct {
		Cookies []chromiumCookie `json:"cookies"`
	}{Cookies: cookies}, nil)
}

func sameTransferOrigin(a, b string) bool {
	first, firstErr := url.Parse(a)
	second, secondErr := url.Parse(b)
	if firstErr != nil || secondErr != nil {
		return false
	}
	firstPort, secondPort := first.Port(), second.Port()
	if firstPort == "" {
		if strings.EqualFold(first.Scheme, "https") {
			firstPort = "443"
		} else {
			firstPort = "80"
		}
	}
	if secondPort == "" {
		if strings.EqualFold(second.Scheme, "https") {
			secondPort = "443"
		} else {
			secondPort = "80"
		}
	}
	return strings.EqualFold(first.Scheme, second.Scheme) && strings.EqualFold(first.Hostname(), second.Hostname()) && firstPort == secondPort
}

func storageTransferScripts(scripts bridge.TargetScriptConfig, targetURL string, state artemisrouter.BrowserState) (bridge.TargetScriptConfig, error) {
	if len(state.LocalStorage) == 0 && len(state.SessionStorage) == 0 {
		return scripts, nil
	}
	if state.URL == "" || !sameTransferOrigin(state.URL, targetURL) {
		return scripts, fmt.Errorf("storage transfer requires the original origin")
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return scripts, err
	}
	encoded, err := json.Marshal(struct {
		Origin  string            `json:"origin"`
		Local   map[string]string `json:"local"`
		Session map[string]string `json:"session"`
	}{Origin: parsed.Scheme + "://" + parsed.Host, Local: state.LocalStorage, Session: state.SessionStorage})
	if err != nil {
		return scripts, err
	}
	scripts.PageScript += "\n(() => { const state=" + string(encoded) + "; if(location.origin !== new URL(state.origin).origin)return; for(const [key,value] of Object.entries(state.local||{}))localStorage.setItem(key,value); for(const [key,value] of Object.entries(state.session||{}))sessionStorage.setItem(key,value); })();"
	digest := sha256.Sum256([]byte(scripts.PageScript + scripts.WorkerScript))
	scripts.Version = hex.EncodeToString(digest[:])
	return scripts, nil
}
