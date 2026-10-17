package serve

import (
	"context"
	"encoding/json"

	"github.com/Christopher-Schulze/Artemis"
)

// pageResp answers with a page-derived value labeled by the page's current
// origin and the IDPI verdict over the encoded value (spec L4202).
func (s *Server) pageResp(ctx context.Context, req *Request, session *artemis.Session, pageID, source string, value any) *Response {
	pageURL, taskErr := session.PageURL(ctx, pageID)
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrNoPage)
	}
	return s.labeledResp(req, pageURL, source, value)
}

// labeledResp encodes value once, labels it and carries the encoded bytes.
// Policy-blocked content never reaches the client.
func (s *Server) labeledResp(req *Request, pageURL, source string, value any) *Response {
	raw, err := json.Marshal(value)
	if err != nil {
		return errResp(req.ID, string(ErrExecutionFailed), "encode result: "+err.Error())
	}
	trust, err := s.idpi.Label(pageURL, source, raw)
	if err != nil {
		return errResp(req.ID, string(ErrContentBlocked), err.Error())
	}
	return &Response{ID: req.ID, OK: true, Value: json.RawMessage(raw), Trust: &trust}
}
