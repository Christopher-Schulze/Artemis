package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"mime"
	"net/http"
	"strings"

	"github.com/Christopher-Schulze/Artemis/js"
	"github.com/Christopher-Schulze/Artemis/network"
	"github.com/Christopher-Schulze/Artemis/parser"
	"github.com/Christopher-Schulze/Artemis/webapi"
)

// ErrUnsupportedRepresentation is returned for responses the renderless
// engine cannot present as a document (images, archives, binaries); callers
// use Download or a browser mode instead of a misparsed page.
var ErrUnsupportedRepresentation = errors.New("engine: response representation is not a document")

// documentSource returns the markup to parse for resp: HTML as served, other
// text (text/plain, JSON, CSS, JavaScript, XML) as literal text inside <pre>,
// so markup in a plaintext body never gains document or script semantics.
func documentSource(resp *network.Response) ([]byte, error) {
	contentType := resp.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(resp.Body)
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	}
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		return resp.Body, nil
	case strings.HasPrefix(mediaType, "text/") || mediaType == "application/json" || mediaType == "application/javascript" || mediaType == "application/xml" || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml"):
		return []byte("<!doctype html><html><head></head><body><pre>" + html.EscapeString(string(resp.Body)) + "</pre></body></html>"), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedRepresentation, mediaType)
	}
}

func (e *Engine) pageFromResponse(ctx context.Context, opts FetchOpts, sessionID string, resp *network.Response) (*Page, error) {
	source, err := documentSource(resp)
	if err != nil {
		return nil, err
	}
	doc, err := parser.ParseHTML(bytes.NewReader(source), resp.FinalURL)
	if err != nil {
		return nil, fmt.Errorf("engine: parse %s: %w", resp.FinalURL, err)
	}

	jar, err := e.client.CookieJarForSession(sessionID)
	if err != nil {
		return nil, err
	}
	scope := newAdBlockScope(resp.FinalURL, opts)
	jsCtx, err := e.jsRT.NewContext(doc, js.ContextOpts{
		Console:        opts.Console,
		Fetch:          e.jsFetchFunc(sessionID, scope),
		AsyncFetch:     opts.AsyncFetch,
		Navigator:      e.navigatorDefaults(opts.Navigator),
		CookieJar:      jar,
		LoadStylesheet: e.stylesheetLoader(resp.FinalURL, sessionID, scope),
		LoadIFrame:     e.iframeLoader(resp.FinalURL, sessionID, scope),
		RequestContext: network.WithAdBlockScope(ctx, scope),
		Policy:         e.policy,
		Proxy:          e.client.ProxyURL(),
		SessionID:      sessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("engine: js context: %w", err)
	}
	return e.admitResponsePage(ctx, opts, sessionID, resp, doc, jsCtx, scope)
}

func (e *Engine) admitResponsePage(ctx context.Context, opts FetchOpts, sessionID string, resp *network.Response, doc *webapi.Document, jsCtx *js.Context, scope network.AdBlockScope) (*Page, error) {
	if err := e.session.acquireTab(sessionID); err != nil {
		jsCtx.Close()
		return nil, err
	}

	page := &Page{
		url:        resp.FinalURL,
		statusCode: resp.StatusCode,
		headers:    resp.Headers.Clone(),
		document:   doc,
		rawBody:    append([]byte(nil), resp.Body...),
		jsCtx:      jsCtx,
		download: func(filename, contentType string, content []byte) (*Download, error) {
			return e.storePageDownload(sessionID, filename, contentType, content)
		},
		session:   e.session,
		sessionID: sessionID,
		adBlocked: scope.Blocked,
	}

	if opts.RunInlineScripts || opts.RunScripts {
		if err := e.runScripts(ctx, page, jsCtx, doc, resp.FinalURL, scope); err != nil {
			return nil, errors.Join(err, page.Close())
		}
	}

	return page, nil
}
