package artemis

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Christopher-Schulze/Artemis/bridge"
	"github.com/Christopher-Schulze/Artemis/bridge/actions"
	"github.com/Christopher-Schulze/Artemis/engine"
	artemisrouter "github.com/Christopher-Schulze/Artemis/router"
)

const maxTaskExpressionBytes = 64 << 10
const maxTaskEvaluationBytes = 64 << 10

func validateTaskExpression(expression string) *TaskError {
	if len(expression) > maxTaskExpressionBytes || !utf8.ValidString(expression) || strings.ContainsRune(expression, 0) ||
		expression != "" && strings.TrimSpace(expression) == "" {
		return newTaskError(TaskErrorInvalidInput, "evaluate", fmt.Errorf("bounded nonempty UTF-8 expression required"))
	}
	return nil
}

func taskStringExpression(expression string) string {
	return fmt.Sprintf("((value) => { const text = `${value}`; if (text.length > %d) throw new RangeError('task evaluation output exceeds limit'); return text; })((%s\n))", maxTaskEvaluationBytes, expression)
}

func finishTaskRoute(ctx context.Context, lease executionLease, action FetchAction, route *artemisrouter.RouteResult) (data *PageResult, returnErr error) {
	defer func() {
		returnErr = errors.Join(returnErr, route.Close())
		if returnErr != nil {
			data = nil
		}
	}()
	if action.EvaluateExpression == "" {
		return taskRouteProjection(route.Output), nil
	}
	execCtx, cancel := context.WithTimeout(ctx, lease.config.ScriptTimeout)
	defer cancel()
	data, evaluation, err := evaluateTaskResource(execCtx, lease.owner, route.Resource, taskStringExpression(action.EvaluateExpression))
	if err != nil {
		return nil, err
	}
	if err := execCtx.Err(); err != nil {
		return nil, err
	}
	if len(evaluation) > maxTaskEvaluationBytes || !utf8.ValidString(evaluation) {
		return nil, fmt.Errorf("task evaluation output exceeds UTF-8 byte limit")
	}
	data.Evaluation = &evaluation
	return data, nil
}

func evaluateTaskResource(ctx context.Context, owner *Agent, resource artemisrouter.Resource, expression string) (*PageResult, string, error) {
	switch page := resource.(type) {
	case *engine.Page:
		if err := owner.authorizeBrowserRequest(ctx, bridge.PolicyRequest{Action: "evaluate", URL: page.URL()}); err != nil {
			return nil, "", err
		}
		evaluation, err := page.EvalString(ctx, expression)
		if err != nil {
			return nil, "", err
		}
		return taskEngineProjection(page), evaluation, nil
	case *chromiumRouteResource:
		return page.evaluateTask(ctx, expression)
	default:
		return nil, "", newTaskError(TaskErrorCapabilityUnavailable, "evaluate", fmt.Errorf("retained route has no owned evaluation page"))
	}
}

func (r *chromiumRouteResource) evaluateTask(ctx context.Context, expression string) (*PageResult, string, error) {
	if r == nil || r.page == nil || r.session == nil {
		return nil, "", newTaskError(TaskErrorStaleReference, "evaluate", fmt.Errorf("native route is closed"))
	}
	if err := r.session.authorizeChromiumOperation(ctx, r.page, "evaluate", ""); err != nil {
		return nil, "", err
	}
	outcome := r.page.actions.Execute(ctx, actions.Request{Kind: actions.KindEvaluate, Expression: expression, Timeout: r.session.owner.Config().ScriptTimeout})
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if !outcome.Success {
		code := TaskErrorExecutionFailed
		if outcome.Failure == actions.FailurePolicy {
			code = TaskErrorPolicyDenied
		}
		return nil, "", newTaskError(code, "evaluate", fmt.Errorf("native evaluation: %s: %s", outcome.Failure, outcome.Error))
	}
	value, ok := outcome.Value.(string)
	if !ok {
		return nil, "", fmt.Errorf("native task evaluation did not return a string")
	}
	output, err := (artemisrouter.ChromiumExecutor{Page: r.page.page, Observer: r.page.observer}).Read(ctx, artemisrouter.BrowserState{SessionID: r.session.id})
	if err != nil {
		return nil, "", err
	}
	return taskRouteProjection(output.Page), value, nil
}

func taskRouteProjection(output artemisrouter.PageOutput) *PageResult {
	return &PageResult{URL: output.URL, StatusCode: output.StatusCode, Title: output.Title,
		HTML: output.HTML, Text: output.Text, Markdown: output.Markdown, Links: output.Links}
}

func taskEngineProjection(page *engine.Page) *PageResult {
	return &PageResult{URL: page.URL(), StatusCode: page.StatusCode(), Title: page.Title(), HTML: page.HTML(),
		Text: page.Text(), Markdown: page.Markdown(), Links: page.Links()}
}
