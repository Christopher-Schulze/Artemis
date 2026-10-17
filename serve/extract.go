package serve

import (
	"context"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/scraper"
)

func (s *Server) cmdPageExtract(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params PageExtractParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	if (params.Schema == nil) == (params.JSON == nil) || params.JSON != nil && params.Pagination != nil {
		return errResp(req.ID, string(ErrBadParams), "page.extract needs exactly one of schema or json; pagination applies to schema")
	}
	var result *scraper.GroundedExtraction
	var taskErr *artemis.TaskError
	switch {
	case params.JSON != nil:
		result, taskErr = session.ExtractResponseJSON(ctx, params.PageID, *params.JSON)
	case params.Pagination != nil:
		result, taskErr = session.ExtractGroundedPages(ctx, params.PageID, params.Schema, *params.Pagination)
	default:
		result, taskErr = session.ExtractGrounded(ctx, params.PageID, params.Schema)
	}
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "page.extract", result)
}
