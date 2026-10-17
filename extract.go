package artemis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/scraper"
	"github.com/Christopher-Schulze/Artemis/security"
)

// extract.go connects source-grounded structured extraction (scraper.
// ExtractGroundedHTML / ExtractGroundedJSON) to session pages: the captured
// document, its URL, capture time and trust label are bound to every field.
// Pagination is bounded and same-origin; JSON responses are fetched by the
// page itself under its origin, so cookies and tokens never leave the
// browser.

const (
	maxExtractHTMLBytes     = 8 << 20
	maxExtractResponseBytes = 4 << 20
	defaultExtractPages     = 10
	maxExtractPages         = 50
)

// Pagination stop reasons.
const (
	ExtractStopNoNextPage  = "no_next_page"
	ExtractStopCycleClosed = "cycle_closed"
	ExtractStopMaxPages    = "max_pages"
	ExtractStopLeftOrigin  = "next_page_leaves_origin"
)

// ExtractPagination follows the href of NextSelector up to MaxPages pages
// (default 10, at most 50) on the page's origin.
type ExtractPagination struct {
	NextSelector string `json:"nextSelector"`
	MaxPages     int    `json:"maxPages,omitempty"`
}

// JSONResponseRequest extracts Schema from the same-origin JSON resource URL.
type JSONResponseRequest struct {
	URL    string                    `json:"url"`
	Schema scraper.JSONExtractSchema `json:"schema"`
}

// ExtractGrounded extracts schema from the page's current document with
// per-field source evidence and the page's trust label.
func (s *Session) ExtractGrounded(ctx context.Context, pageID string, schema *scraper.StructuredSchema) (*scraper.GroundedExtraction, *TaskError) {
	document, source, taskErr := s.captureExtractSource(ctx, pageID)
	if taskErr != nil {
		return nil, taskErr
	}
	result, err := scraper.ExtractGroundedHTML(document, schema, source)
	if err != nil {
		return nil, newTaskError(TaskErrorInvalidInput, "extract", err)
	}
	return result, nil
}

// ExtractGroundedPages extracts schema from the page and every page reached
// by its next link. Completeness requires that pagination ended because no
// further page exists.
func (s *Session) ExtractGroundedPages(ctx context.Context, pageID string, schema *scraper.StructuredSchema, pagination ExtractPagination) (merged *scraper.GroundedExtraction, taskErr *TaskError) {
	if pagination.MaxPages < 0 || pagination.MaxPages > maxExtractPages {
		return nil, newTaskError(TaskErrorInvalidInput, "extract_pages", fmt.Errorf("maxPages must be 0..%d", maxExtractPages))
	}
	if pagination.MaxPages == 0 {
		pagination.MaxPages = defaultExtractPages
	}
	next, err := cascadia.Compile(pagination.NextSelector)
	if err != nil || strings.TrimSpace(pagination.NextSelector) == "" {
		return nil, newTaskError(TaskErrorInvalidInput, "extract_pages", fmt.Errorf("invalid next selector: %v", err))
	}
	var pages []*scraper.GroundedExtraction
	seen := map[string]bool{}
	currentPage := pageID
	renderless := s.Page(pageID) != nil
	// Renderless pagination opens each further page as its own session page
	// and closes it once the next one is open or the walk ends.
	defer func() {
		if !renderless || currentPage == pageID {
			return
		}
		if closeErr := s.ClosePage(currentPage); closeErr != nil && taskErr == nil {
			merged, taskErr = nil, closeErr
		}
	}()
	for {
		document, source, taskErr := s.captureExtractSource(ctx, currentPage)
		if taskErr != nil {
			return nil, taskErr
		}
		seen[source.URL] = true
		page, err := scraper.ExtractGroundedHTML(document, schema, source)
		if err != nil {
			return nil, newTaskError(TaskErrorInvalidInput, "extract_pages", err)
		}
		pages = append(pages, page)
		target, found := nextPageURL(document, next, source.URL)
		switch {
		case !found:
			return scraper.MergeGroundedPages(pages, ExtractStopNoNextPage, true), nil
		case seen[target]:
			return scraper.MergeGroundedPages(pages, ExtractStopCycleClosed, true), nil
		case !sameOrigin(target, source.URL):
			return scraper.MergeGroundedPages(pages, ExtractStopLeftOrigin, false), nil
		case len(pages) == pagination.MaxPages:
			return scraper.MergeGroundedPages(pages, ExtractStopMaxPages, false), nil
		}
		if renderless {
			opened, _, taskErr := s.OpenPage(ctx, target, true)
			if taskErr != nil {
				return nil, taskErr
			}
			previous := currentPage
			currentPage = opened
			if previous != pageID {
				if taskErr := s.ClosePage(previous); taskErr != nil {
					return nil, taskErr
				}
			}
			continue
		}
		outcome, taskErr := s.ActChromiumPage(ctx, pageID, 0, actions.Request{Kind: actions.KindNavigate, URL: target})
		if taskErr != nil {
			return nil, taskErr
		}
		if !outcome.Success {
			return nil, newTaskError(TaskErrorExecutionFailed, "extract_pages", fmt.Errorf("navigate to %s: %s", target, outcome.Error))
		}
	}
}

// ExtractResponseJSON fetches a same-origin JSON resource from inside the
// page (credentials stay in the browser, redirects are refused) and
// extracts schema from it with source evidence.
func (s *Session) ExtractResponseJSON(ctx context.Context, pageID string, request JSONResponseRequest) (*scraper.GroundedExtraction, *TaskError) {
	if err := request.Schema.Validate(); err != nil {
		return nil, newTaskError(TaskErrorInvalidInput, "extract_json", err)
	}
	if s.Page(pageID) != nil {
		return nil, newTaskError(TaskErrorCapabilityUnavailable, "extract_json", errors.New("response extraction runs in a Chromium page; open the page with Chromium"))
	}
	execCtx, done, taskErr := s.chromiumOperation(ctx)
	if taskErr != nil {
		return nil, taskErr
	}
	defer done()
	page, lease, taskErr := s.leaseChromiumPage(execCtx, pageID)
	if taskErr != nil {
		return nil, taskErr
	}
	defer lease.Release()
	execCtx = lease.Context()
	pageURL, err := page.page.CurrentURL(execCtx)
	if err != nil {
		return nil, classifyTaskError("extract_json", err)
	}
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, classifyTaskError("extract_json", err)
	}
	reference, err := url.Parse(request.URL)
	if err != nil {
		return nil, newTaskError(TaskErrorInvalidInput, "extract_json", err)
	}
	target := base.ResolveReference(reference).String()
	if !sameOrigin(target, pageURL) {
		return nil, newTaskError(TaskErrorPolicyDenied, "extract_json", fmt.Errorf("response %s is outside the page origin", target))
	}
	if err := s.owner.authorizeBrowserRequest(execCtx, bridge.PolicyRequest{Action: "extract", URL: target, Method: "GET"}); err != nil {
		return nil, classifyTaskError("extract_json", err)
	}
	body, truncated, taskErr := fetchFromPage(execCtx, page, target)
	if taskErr != nil {
		return nil, taskErr
	}
	trust, err := security.NewIDPIChecker(security.DefaultIDPIConfig()).Label(target, "extract_json", body)
	if err != nil {
		return nil, classifyTaskError("extract_json", err)
	}
	result, err := scraper.ExtractGroundedJSON(body, request.Schema, scraper.ExtractionSource{URL: target, CapturedAt: time.Now().UTC(), Truncated: truncated, Trust: trust})
	if err != nil {
		return nil, newTaskError(TaskErrorInvalidInput, "extract_json", err)
	}
	return result, nil
}

// captureExtractSource reads the page document through the existing dump
// owner, bounds it and labels its trust.
func (s *Session) captureExtractSource(ctx context.Context, pageID string) (string, scraper.ExtractionSource, *TaskError) {
	var dumped any
	var taskErr *TaskError
	if s.Page(pageID) != nil {
		dumped, taskErr = s.Dump(pageID, "html")
	} else {
		dumped, taskErr = s.DumpChromiumPage(ctx, pageID, "html")
	}
	if taskErr != nil {
		return "", scraper.ExtractionSource{}, taskErr
	}
	document, ok := dumped.(string)
	if !ok {
		return "", scraper.ExtractionSource{}, newTaskError(TaskErrorExecutionFailed, "extract", fmt.Errorf("page document is %T", dumped))
	}
	pageURL, taskErr := s.PageURL(ctx, pageID)
	if taskErr != nil {
		return "", scraper.ExtractionSource{}, taskErr
	}
	source := scraper.ExtractionSource{URL: pageURL, CapturedAt: time.Now().UTC()}
	if len(document) > maxExtractHTMLBytes {
		document, source.Truncated = document[:maxExtractHTMLBytes], true
	}
	trust, err := security.NewIDPIChecker(security.DefaultIDPIConfig()).Label(pageURL, "extract", []byte(document))
	if err != nil {
		return "", scraper.ExtractionSource{}, classifyTaskError("extract", err)
	}
	source.Trust = trust
	return document, source, nil
}

func nextPageURL(document string, selector cascadia.Selector, base string) (string, bool) {
	root, err := html.Parse(strings.NewReader(document))
	if err != nil {
		return "", false
	}
	link := cascadia.Query(root, selector)
	if link == nil {
		return "", false
	}
	for _, attribute := range link.Attr {
		if attribute.Key != "href" || strings.TrimSpace(attribute.Val) == "" {
			continue
		}
		baseURL, err := url.Parse(base)
		if err != nil {
			return "", false
		}
		reference, err := url.Parse(strings.TrimSpace(attribute.Val))
		if err != nil {
			return "", false
		}
		resolved := baseURL.ResolveReference(reference)
		resolved.Fragment = ""
		return resolved.String(), true
	}
	return "", false
}

func sameOrigin(left, right string) bool {
	a, errA := url.Parse(left)
	b, errB := url.Parse(right)
	return errA == nil && errB == nil && a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

type pageFetchResult struct {
	OK          bool   `json:"ok"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	Error       string `json:"error"`
}

// fetchFromPage runs fetch in a fresh isolated world of the main frame, so
// page scripts cannot replace it; only status, content type and the bounded
// body cross back.
func fetchFromPage(ctx context.Context, page *ownedChromiumPage, target string) ([]byte, bool, *TaskError) {
	var world struct {
		ExecutionContextID int64 `json:"executionContextId"`
	}
	frameID := page.page.TargetID()
	if err := page.page.Call(ctx, "Page.createIsolatedWorld", struct {
		FrameID   string `json:"frameId"`
		WorldName string `json:"worldName"`
	}{frameID, "artemis-extract"}, &world); err != nil || world.ExecutionContextID == 0 {
		return nil, false, newTaskError(TaskErrorExecutionFailed, "extract_json", fmt.Errorf("isolated world: %v", err))
	}
	encodedURL, err := json.Marshal(target)
	if err != nil {
		return nil, false, classifyTaskError("extract_json", err)
	}
	expression := `fetch(` + string(encodedURL) + `,{credentials:"same-origin",redirect:"error",headers:{"Accept":"application/json"}}).then(async r=>{const t=await r.text();return {ok:r.ok,status:r.status,contentType:r.headers.get("content-type")||"",body:t.slice(0,` + strconv.Itoa(maxExtractResponseBytes+1) + `)}}).catch(e=>({error:String(e)}))`
	var evaluated struct {
		Result struct {
			Value pageFetchResult `json:"value"`
		} `json:"result"`
		ExceptionDetails json.RawMessage `json:"exceptionDetails"`
	}
	if err := page.page.Call(ctx, "Runtime.evaluate", struct {
		Expression    string `json:"expression"`
		ContextID     int64  `json:"contextId"`
		ReturnByValue bool   `json:"returnByValue"`
		AwaitPromise  bool   `json:"awaitPromise"`
	}{expression, world.ExecutionContextID, true, true}, &evaluated); err != nil {
		return nil, false, classifyTaskError("extract_json", err)
	}
	response := evaluated.Result.Value
	switch {
	case len(evaluated.ExceptionDetails) > 0 || response.Error != "":
		return nil, false, newTaskError(TaskErrorExecutionFailed, "extract_json", fmt.Errorf("fetch failed: %s", response.Error))
	case !response.OK:
		return nil, false, newTaskError(TaskErrorExecutionFailed, "extract_json", fmt.Errorf("response status %d", response.Status))
	case !strings.Contains(strings.ToLower(response.ContentType), "json"):
		return nil, false, newTaskError(TaskErrorInvalidInput, "extract_json", fmt.Errorf("response content type %q is not JSON", response.ContentType))
	}
	body := []byte(response.Body)
	if len(body) > maxExtractResponseBytes {
		return body[:maxExtractResponseBytes], true, nil
	}
	return body, false, nil
}
