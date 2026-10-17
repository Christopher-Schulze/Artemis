package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
)

func (s *Server) cmdChromiumQuery(ctx context.Context, client clientIdentity, req *Request) *Response {
	if raw := bytes.TrimSpace(req.Params); len(raw) == 0 || raw[0] != '{' {
		return errResp(req.ID, string(ErrBadParams), "query requires exactly one parameter object")
	}
	var params ChromiumQueryParams
	decoder := json.NewDecoder(bytes.NewReader(req.Params))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errResp(req.ID, string(ErrBadParams), "query requires exactly one parameter object")
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	result, err := session.QueryChromiumPage(ctx, params.PageID, params.Request)
	if err != nil {
		return s.taskErrorResponse(req, err, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "chromium.query", result)
}
