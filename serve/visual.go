package serve

import (
	"context"

	"github.com/Christopher-Schulze/Artemis"
)

func (s *Server) cmdChromiumVisual(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumVisualParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	observation, taskErr := session.CaptureVisual(ctx, params.PageID, params.Options)
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "chromium.visual", observation)
}

func (s *Server) cmdChromiumVisualAct(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumVisualActParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	if params.LeaseToken != "" {
		ctx = artemis.WithWriterLease(ctx, params.LeaseToken)
	}
	result, taskErr := session.ActVisual(ctx, params.PageID, params.Request)
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "chromium.visual_act", result)
}

func (s *Server) cmdChromiumVisualGround(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params ChromiumVisualGroundParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	point, taskErr := session.GroundVisual(ctx, params.PageID, params.ObservationID, params.Instruction)
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "chromium.visual_ground", point)
}
