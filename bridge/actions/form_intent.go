package actions

import (
	"context"
	"errors"
	"fmt"

	formactions "github.com/Christopher-Schulze/Artemis/actions"
	"github.com/Christopher-Schulze/Artemis/bridge"
)

type pageFormEventStream struct {
	subscription *bridge.CDPSubscription
}

func NewPageFormIntentRuntime(page *bridge.Page, metricSink func(formactions.FormIntentMetricEvent)) (*formactions.FormIntentRuntime, error) {
	if page == nil {
		return nil, errors.New("page form intent runtime: page required")
	}
	return formactions.NewCDPFormIntentRuntime(formactions.CDPFormIntentConfig{
		Caller: page, SessionID: page.SessionID(), PageID: page.TargetID(),
		Subscribe: func(buffer int) (formactions.FormCDPEventStream, error) {
			subscription, err := page.SubscribeBrowserEvents(buffer)
			if err != nil {
				return nil, err
			}
			return &pageFormEventStream{subscription: subscription}, nil
		},
	}, formactions.FormIntentRuntimeConfig{MetricSink: metricSink})
}

func (s *pageFormEventStream) Next(ctx context.Context) (formactions.FormCDPEvent, error) {
	if s == nil || s.subscription == nil {
		return formactions.FormCDPEvent{}, errors.New("page form intent event stream unavailable")
	}
	select {
	case event, ok := <-s.subscription.Events:
		if !ok {
			return formactions.FormCDPEvent{}, errors.New("page form intent event stream closed")
		}
		return formactions.FormCDPEvent{Method: event.Method, Params: event.Params, SessionID: event.SessionID}, nil
	case err, ok := <-s.subscription.Errors:
		if !ok || err == nil {
			return formactions.FormCDPEvent{}, errors.New("page form intent event stream closed")
		}
		return formactions.FormCDPEvent{}, err
	case <-ctx.Done():
		return formactions.FormCDPEvent{}, ctx.Err()
	}
}

func (s *pageFormEventStream) Close() {
	if s != nil && s.subscription != nil {
		s.subscription.Close()
	}
}

func (r *Runtime) fillFormIntent(ctx context.Context, request Request, evidence Evidence) Outcome {
	if r.formIntents == nil || request.FormIntent == nil {
		return failedNow(evidence, FailureValidation, "form intent runtime unavailable")
	}
	result, err := r.formIntents.Execute(ctx, *request.FormIntent)
	if err != nil {
		return failedNow(evidence, classifyContext(ctx, FailureProtocol), fmt.Sprintf("fill form: %v", err))
	}
	evidence.Ref = request.FormIntent.FormRoot
	evidence.Postcondition = Postcondition{Type: "fields_filled", Expected: len(request.FormIntent.Fields), Actual: result.FieldsFilled, Passed: result.FieldsFilled == len(request.FormIntent.Fields)}
	return Outcome{Success: evidence.Postcondition.Passed, Value: result, Evidence: evidence}
}

func (r *Runtime) FormIntentMetrics() formactions.FormIntentMetrics {
	if r == nil || r.formIntents == nil {
		return formactions.FormIntentMetrics{}
	}
	return r.formIntents.Metrics()
}

func (r *Runtime) invalidateFormIntentPage() error {
	if r == nil || r.formIntents == nil || r.page == nil {
		return nil
	}
	return r.formIntents.InvalidatePage(r.page.SessionID(), r.page.TargetID())
}

func (r *Runtime) Close() error {
	if r == nil || r.formIntents == nil {
		return nil
	}
	return r.formIntents.Close()
}
