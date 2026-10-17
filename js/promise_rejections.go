package js

import (
	"errors"
	"sort"
	"strings"

	v8 "github.com/tommie/v8go"
)

// UnhandledPromiseRejectionError reports native unhandled rejection diagnostics
// at the owning operation boundary, after its current microtasks and callbacks.
type UnhandledPromiseRejectionError struct{ Message string }

func (e *UnhandledPromiseRejectionError) Error() string {
	return "unhandled promise rejection: " + e.Message
}

func (r *Runtime) observePromiseRejection(realm *v8.Context) {
	if r.execution == nil {
		return
	}
	if owner := r.contextFor(realm); owner != nil {
		r.execution.observePromiseContext(owner)
	}
}

func (s *executionSpan) observePromiseContext(owner *Context) {
	if s.promiseContexts == nil {
		s.promiseContexts = make(map[*Context]struct{})
	}
	if _, exists := s.promiseContexts[owner]; exists {
		return
	}
	if len(s.promiseContexts) >= maxCallbackReturnValues {
		s.recordFailure(errors.New("promise rejection context limit exceeded"))
		return
	}
	s.promiseContexts[owner] = struct{}{}
}

func (s *executionSpan) takePromiseRejections() error {
	var diagnostics []string
	var failure error
	omitted := 0
	for owner := range s.promiseContexts {
		if owner.v8ctx.IsClosed() {
			continue
		}
		message, err := owner.v8ctx.TakePromiseRejections()
		failure = errors.Join(failure, err)
		if message != "" {
			owner.pageExposed.Store(true)
			diagnostics = append(diagnostics, message)
			sort.Strings(diagnostics)
			if len(diagnostics) > 16 {
				diagnostics[16] = ""
				diagnostics = diagnostics[:16]
				omitted++
			}
		}
	}
	clear(s.promiseContexts)
	if len(diagnostics) == 0 {
		return failure
	}
	sort.Strings(diagnostics)
	if omitted > 0 {
		diagnostics = append(diagnostics, "Additional rejection contexts omitted")
	}
	return errors.Join(failure, &UnhandledPromiseRejectionError{Message: strings.Join(diagnostics, "\n")})
}
