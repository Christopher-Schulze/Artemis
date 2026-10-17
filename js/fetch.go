package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/net/http/httpguts"

	v8 "github.com/tommie/v8go"
)

// FetchRequest is the input to a FetchFunc, decoded from the JS-side
// fetch(url, opts) call.
type FetchRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

// FetchResponse is the output of a FetchFunc.
type FetchResponse struct {
	Status     int
	StatusText string
	Headers    map[string][]string
	Body       []byte
	URL        string
}

// FetchFunc performs the HTTP request behind a JS fetch() call. It is
// invoked on the V8 thread in synchronous mode or on an owned worker in
// asynchronous mode. It must honor ctx cancellation and return after transport
// cleanup; Context.Close joins that return. A nil FetchFunc disables fetch().
type FetchFunc func(ctx context.Context, req FetchRequest) (*FetchResponse, error)

// fetchTemplates caches the fetch global + thrower + the per-response
// text/json method templates at Runtime level. Per-response state (body
// bytes) is held in a Runtime-level slab; the response Object's internal
// field 0 holds the slab handle so the cached text/json callbacks can
// retrieve the right body.
type fetchTemplates struct {
	thrower     *v8.FunctionTemplate
	fetchFn     *v8.FunctionTemplate
	abortFn     *v8.FunctionTemplate
	respObjTmpl *v8.ObjectTemplate
	respText    *v8.FunctionTemplate
	respBuffer  *v8.FunctionTemplate
	respUsed    *v8.FunctionTemplate
	respClone   *v8.FunctionTemplate
	respDiscard *v8.FunctionTemplate
}

// fetchBodyHandles is a per-Runtime slab of fetch response bodies keyed
// by uint32 handles. The handle is stored in the response Object's
// internal field 0; cached text/json templates resolve it.
type fetchBodyHandles struct {
	seq atomic.Uint32
	m   sync.Map // uint32 -> *fetchBody
}

func (h *fetchBodyHandles) put(body *fetchBody) (uint32, error) {
	for {
		previous := h.seq.Load()
		if previous >= math.MaxInt32 {
			return 0, errors.New("fetch body handle space exhausted")
		}
		if h.seq.CompareAndSwap(previous, previous+1) {
			h.m.Store(previous+1, body)
			return previous + 1, nil
		}
	}
}
func (h *fetchBodyHandles) get(id uint32) *fetchBody {
	v, _ := h.m.Load(id)
	if v == nil {
		return nil
	}
	return v.(*fetchBody)
}
func (h *fetchBodyHandles) remove(id uint32) {
	if value, exists := h.m.LoadAndDelete(id); exists {
		value.(*fetchBody).releasePayload()
		value.(*fetchBody).releaseSignal()
	}
}

func (r *Runtime) ensureFetchTemplates() (*fetchTemplates, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetchTemplates != nil {
		return r.fetchTemplates, nil
	}
	if r.fetchBodies == nil {
		r.fetchBodies = &fetchBodyHandles{}
	}
	iso := r.iso
	respTmpl := v8.NewObjectTemplate(iso)
	respTmpl.SetInternalFieldCount(2)
	templates := &fetchTemplates{
		respObjTmpl: respTmpl,
		abortFn:     v8.NewFunctionTemplate(iso, r.abortFetch),
		thrower: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			return r.responseError(info, errors.New("fetch is not configured for this Context"))
		}),
		fetchFn: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) (result *v8.Value) {
			defer info.Release()
			c := r.contextFor(info.Context())
			args := info.Args()
			resolver, err := v8.NewPromiseResolver(info.Context())
			if err != nil {
				return r.responseError(info, fmt.Errorf("fetch: cannot create promise: %w", err))
			}
			retained := false
			defer func() {
				if !retained {
					resolver.Release()
				}
				if c != nil && result != nil {
					result = c.callbackResult(result)
				}
			}()
			if c == nil || c.fetcher == nil {
				rejectErr(iso, resolver, errors.New("fetch is not configured"))
				return resolver.GetPromise().Value
			}
			if len(args) == 0 {
				rejectErr(iso, resolver, errors.New("fetch: url required"))
				return resolver.GetPromise().Value
			}
			req, err := c.parseFetchArgs(args)
			if err == nil {
				req.URL, err = c.resolveFetchURL(req.URL)
			}
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			signal, err := fetchSignalArgument(args)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			if signal != nil {
				defer signal.Release()
			}
			if c.asyncFetch {
				retained = c.asyncFetchPath(req, resolver, signal)
				return resolver.GetPromise().Value
			}
			fetchCtx, cancel := c.fetchContext()
			watch, err := c.watchFetchSignal(signal, cancel)
			if err != nil {
				cancel()
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			defer watch.retire(false)
			defer cancel()
			if aborted, err := watch.rejectAborted(resolver); aborted || err != nil {
				if err != nil {
					rejectErr(iso, resolver, err)
				}
				return resolver.GetPromise().Value
			}
			resp, err := c.fetcher(fetchCtx, req)
			cancel()
			if aborted, signalErr := watch.rejectAborted(resolver); aborted || signalErr != nil {
				if signalErr != nil {
					rejectErr(iso, resolver, signalErr)
				}
				return resolver.GetPromise().Value
			}
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			respObj, bodyID, err := buildResponseObject(c, info.Context(), resp, watch)
			if err != nil {
				rejectErr(iso, resolver, fmt.Errorf("fetch: build response: %w", err))
				return resolver.GetPromise().Value
			}
			defer respObj.Release()
			if aborted, signalErr := watch.rejectAborted(resolver); aborted || signalErr != nil {
				c.rt.fetchBodies.remove(bodyID)
				if signalErr != nil {
					rejectErr(iso, resolver, signalErr)
				}
				return resolver.GetPromise().Value
			}
			if !resolver.Resolve(respObj) {
				c.rt.fetchBodies.remove(bodyID)
				c.rt.execution.recordFailure(errors.New("fetch: native promise resolution failed"))
			}
			return resolver.GetPromise().Value
		}),
		respText: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return r.consumeResponseBody(info, false)
		}),
		respBuffer: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			return r.consumeResponseBody(info, true)
		}),
		respUsed: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			body, err := r.responseBody(info)
			if err != nil {
				return r.responseError(info, err)
			}
			if body.used {
				return body.owner.responseTrue
			}
			return body.owner.responseFalse
		}),
		respClone: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			body, err := r.responseBody(info)
			if err == nil && body.used {
				err = errors.New("TypeError: response body is already consumed")
			}
			if err != nil {
				return r.responseError(info, err)
			}
			clone := &fetchBody{owner: body.owner, payload: body.payload, null: body.null, signal: body.signal}
			if clone.payload != nil {
				clone.payload.references++
			}
			value, err := body.owner.newResponseBodyObject(clone)
			if err != nil {
				return r.responseError(info, err)
			}
			return body.owner.callbackResult(value)
		}),
		respDiscard: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			body, err := r.responseBody(info)
			if err != nil {
				return r.responseError(info, err)
			}
			r.fetchBodies.remove(body.id)
			return v8.Undefined(r.iso)
		}),
	}
	for _, method := range []struct {
		name     string
		template *v8.FunctionTemplate
	}{{"text", templates.respText}, {"arrayBuffer", templates.respBuffer}, {"__bodyUsed", templates.respUsed}, {"__clone", templates.respClone}, {"__discard", templates.respDiscard}} {
		if err := respTmpl.Set(method.name, method.template); err != nil {
			return nil, err
		}
	}
	r.fetchTemplates = templates
	return templates, nil
}

func installFetch(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t, err := c.rt.ensureFetchTemplates()
	if err != nil {
		return err
	}
	global := v8ctx.Global()
	defer global.Release()
	abort := t.abortFn.GetFunction(v8ctx)
	defer abort.Release()
	if err := global.Set("__fetch_abort", abort); err != nil {
		return err
	}
	fetch := t.fetchFn
	if c.fetcher == nil {
		// fetch() throws if invoked when no fetcher is configured.
		fetch = t.thrower
	}
	function := fetch.GetFunction(v8ctx)
	defer function.Release()
	return global.Set("fetch", function)
}

func (c *Context) parseFetchArgs(args []*v8.Value) (FetchRequest, error) {
	req := FetchRequest{Method: "GET"}
	var err error
	req.URL, err = args[0].StringChecked()
	if err != nil {
		return req, err
	}
	if len(args) < 2 || args[1].IsNullOrUndefined() {
		return req, nil
	}
	optsObj, err := args[1].AsObject()
	if err != nil {
		return req, fmt.Errorf("fetch: invalid options: %w", err)
	}
	if err := parseFetchMethod(optsObj, &req); err != nil {
		return req, err
	}
	if err := c.parseFetchHeaders(optsObj, &req); err != nil {
		return req, err
	}
	if err := c.parseFetchBody(optsObj, &req); err != nil {
		return req, err
	}
	return req, nil
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

// headerMapFromJS extracts header pairs from a JS object via JSON
// round-trip (Object.keys is not exposed by v8go on Object, so we
// stringify and re-parse).
func (c *Context) headerMapFromJS(obj *v8.Object) (map[string]string, error) {
	global := c.v8ctx.Global()
	defer global.Release()
	jsoned, err := c.requestHeaderEncoder.Call(global, obj.Value)
	if err != nil {
		return nil, err
	}
	defer jsoned.Release()
	text := jsoned.String()
	if len(text) > http.DefaultMaxHeaderBytes {
		return nil, errors.New("fetch: request headers exceed limit")
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(text), &headers); err != nil {
		return nil, err
	}
	for name, value := range headers {
		if !httpguts.ValidHeaderFieldName(name) {
			return nil, errors.New("fetch: invalid request header name")
		}
		encoded := make([]byte, 0, len(value))
		for _, character := range value {
			if character > 255 {
				return nil, errors.New("fetch: request header is not a byte string")
			}
			encoded = append(encoded, byte(character))
		}
		if !httpguts.ValidHeaderFieldValue(string(encoded)) {
			return nil, errors.New("fetch: invalid request header value")
		}
		if forbiddenFetchHeader(name, string(encoded)) {
			delete(headers, name)
			continue
		}
		headers[name] = string(encoded)
	}
	return headers, nil
}

// buildResponseObject creates the JS Response shape for a fetch result.
// Uses Runtime-cached templates; the body bytes are stashed in the
// fetchBodies slab and the handle goes in internal field 0 so the
// shared text/json callbacks can dispatch to the right body.
func buildResponseObject(c *Context, v8ctx *v8.Context, r *FetchResponse, signal *fetchSignalWatch) (result *v8.Value, bodyID uint32, returnErr error) {
	metadata, err := responseMetadata(r)
	if err != nil {
		return nil, 0, err
	}
	body, err := c.newResponseBody(r.Body, r.Status == 204 || r.Status == 205 || r.Status == 304)
	if err != nil {
		return nil, 0, err
	}
	if !body.null {
		body.signal = signal
	}
	object, err := c.newResponseBodyObject(body)
	if err != nil {
		return nil, 0, err
	}
	defer object.Release()
	defer func() {
		if returnErr != nil {
			c.rt.fetchBodies.remove(body.id)
		}
	}()
	encoded, err := v8.JSONParse(v8ctx, string(metadata))
	if err != nil {
		return nil, 0, err
	}
	defer encoded.Release()
	global := v8ctx.Global()
	defer global.Release()
	result, returnErr = c.responseFactory.Call(global, object, encoded)
	return result, body.id, returnErr
}

func (c *Context) fetchContext() (context.Context, context.CancelFunc) {
	return borrowContext(c.evalCtx, c.async.ctx)
}

func rejectErr(iso *v8.Isolate, resolver *v8.PromiseResolver, err error) {
	v, valueErr := v8.NewValue(iso, err.Error())
	if valueErr != nil {
		thrown := iso.ThrowException(v8.Null(iso))
		thrown.Release()
		return
	}
	defer v.Release()
	if !resolver.Reject(v) {
		thrown := iso.ThrowException(v)
		thrown.Release()
	}
}
