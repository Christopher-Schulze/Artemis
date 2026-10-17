package serve

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"regexp"
	"time"

	"github.com/Christopher-Schulze/Artemis"
	"github.com/Christopher-Schulze/Artemis/observe"
)

func (s *Server) cmdWorkflowRecord(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params WorkflowRecordParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	switch params.Op {
	case "start":
		if taskErr := session.StartWorkflowRecording(params.PageID, client.id, params.Name, observe.DefaultWorkflowCaptureConfig()); taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, WorkflowRecordResult{})
	case "stop":
		recording, taskErr := session.StopWorkflowRecording(params.PageID)
		if taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		result := WorkflowRecordResult{Recording: recording}
		if len(params.Origins) > 0 {
			definition, err := artemis.WorkflowFromRecording(recording, params.Origins)
			if err != nil {
				return errResp(req.ID, string(ErrBadParams), err.Error())
			}
			result.Definition = &definition
		}
		return s.pageResp(ctx, req, session, params.PageID, "workflow.record", result)
	default:
		return errResp(req.ID, string(ErrBadParams), "workflow.record op must be start or stop")
	}
}

func (s *Server) cmdWorkflowRun(ctx context.Context, client clientIdentity, req *Request) *Response {
	var params WorkflowRunParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	for _, input := range params.Inputs {
		if input.Credential != nil && !params.Shadow {
			return errResp(req.ID, string(ErrCapabilityUnavailable), errors.New("sensitive workflow inputs need a host credential store; run them through the library API").Error())
		}
	}
	options := artemis.WorkflowRunOptions{Shadow: params.Shadow}
	if params.RunID != "" {
		journal, response := s.workflowJournalPath(req, client.id, params.RunID)
		if response != nil {
			return response
		}
		options.Journal = journal
	}
	result, taskErr := session.RunWorkflow(ctx, params.PageID, params.Definition, params.Inputs, options)
	if taskErr != nil {
		return s.taskErrorResponse(req, taskErr, ErrExecutionFailed)
	}
	return s.pageResp(ctx, req, session, params.PageID, "workflow.run", result)
}

var workflowRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// workflowJournalPath scopes a run journal to the requesting client so one
// client can never resume or read another client's run.
func (s *Server) workflowJournalPath(req *Request, clientID, runID string) (string, *Response) {
	if s.opts.WorkflowJournalDir == "" {
		return "", errResp(req.ID, string(ErrCapabilityUnavailable), "journaled workflow runs need a configured journal directory")
	}
	if !workflowRunIDPattern.MatchString(runID) {
		return "", errResp(req.ID, string(ErrBadParams), "runId must be 1-64 letters, digits, '_' or '-'")
	}
	client := sha256.Sum256([]byte(clientID))
	return filepath.Join(s.opts.WorkflowJournalDir, hex.EncodeToString(client[:16]), runID+".jsonl"), nil
}

func (s *Server) cmdPageLease(client clientIdentity, req *Request) *Response {
	var params PageLeaseParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	switch params.Op {
	case "acquire":
		lease, taskErr := session.AcquireWriterLease(params.PageID, params.Holder, time.Duration(params.TTLMillis)*time.Millisecond)
		if taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, lease)
	case "release":
		if taskErr := session.ReleaseWriterLease(params.PageID, params.Token); taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, artemis.WriterLease{})
	default:
		return errResp(req.ID, string(ErrBadParams), "page.lease op must be acquire or release")
	}
}

func (s *Server) cmdPageHandoff(client clientIdentity, req *Request) *Response {
	var params PageHandoffParams
	if err := DecodeParams(req, &params); err != nil {
		return errResp(req.ID, string(ErrBadParams), err.Error())
	}
	session, response := s.ownedSession(client.id, params.SessionID)
	if response != nil {
		return sessionError(req, response)
	}
	switch params.Op {
	case "begin":
		ticket, taskErr := session.BeginHandoff(params.PageID, time.Duration(params.TTLMillis)*time.Millisecond)
		if taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, PageHandoffResult{Ticket: &ticket})
	case "open":
		inspector, taskErr := session.OpenHandoff(params.Token)
		if taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, PageHandoffResult{InspectorURL: inspector})
	case "end":
		if taskErr := session.EndHandoff(params.PageID, params.Token); taskErr != nil {
			return s.taskErrorResponse(req, taskErr, ErrBadParams)
		}
		return okResp(req.ID, PageHandoffResult{})
	default:
		return errResp(req.ID, string(ErrBadParams), "page.handoff op must be begin, open or end")
	}
}
