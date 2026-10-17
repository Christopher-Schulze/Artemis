package serve

import (
	"context"

	bridgeobserve "github.com/Christopher-Schulze/Artemis/bridge/observe"
	"github.com/Christopher-Schulze/Artemis/observe"
	"github.com/Christopher-Schulze/Artemis/security"
)

type ChromiumOpenResult struct {
	PageID   string                      `json:"pageId"`
	Evidence observe.ObservationEvidence `json:"evidence"`
	// AdBlock is the Layer 2 adblocker-wall finding (spec L4208), if any.
	AdBlock *security.AdBlockFinding `json:"adblock,omitempty"`
}

func (s *Server) cmdChromiumOpen(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumOpenParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	pageID, evidence, err := session.OpenChromiumPage(ctx, params.URL)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrFetchFailed)
	}
	return s.pageResp(ctx, req, session, pageID, "chromium.open", ChromiumOpenResult{PageID: pageID, Evidence: evidence, AdBlock: session.AdBlockFinding(pageID)})
}

func (s *Server) cmdChromiumObserve(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumObserveParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	mode := bridgeobserve.Mode(params.Mode)
	if mode == "" {
		mode = bridgeobserve.ModeFull
	}
	evidence, err := session.ObserveChromiumPage(ctx, params.PageID, mode, params.SubtreeRef)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "chromium.observe", evidence)
}

func (s *Server) cmdChromiumClose(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageCloseParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	if err := session.CloseChromiumPage(ctx, params.PageID); err != nil {
		return s.taskErrorResponse(req, err, ErrNoPage)
	}
	return okResp(req.ID, EmptyResult{})
}
