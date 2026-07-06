package js

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	v8 "rogchap.com/v8go"
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
// invoked synchronously on the V8 thread and must return before the
// fetch() Promise resolves. A nil FetchFunc disables fetch().
type FetchFunc func(ctx context.Context, req FetchRequest) (*FetchResponse, error)

// fetchTemplates caches the fetch global + thrower + the per-response
// text/json method templates at Runtime level. Per-response state (body
// bytes) is held in a Runtime-level slab; the response Object's internal
// field 0 holds the slab handle so the cached text/json callbacks can
// retrieve the right body.
type fetchTemplates struct {
	thrower     *v8.FunctionTemplate
	fetchFn     *v8.FunctionTemplate
	respObjTmpl *v8.ObjectTemplate
	respText    *v8.FunctionTemplate
	respJSON    *v8.FunctionTemplate
	headersTmpl *v8.ObjectTemplate
}

// fetchBodyHandles is a per-Runtime slab of fetch response bodies keyed
// by uint32 handles. The handle is stored in the response Object's
// internal field 0; cached text/json templates resolve it.
type fetchBodyHandles struct {
	seq atomic.Uint32
	m   sync.Map // uint32 -> []byte
}

func (h *fetchBodyHandles) put(b []byte) uint32 {
	id := h.seq.Add(1)
	h.m.Store(id, b)
	return id
}
func (h *fetchBodyHandles) get(id uint32) []byte {
	v, _ := h.m.Load(id)
	if v == nil {
		return nil
	}
	return v.([]byte)
}
func (h *fetchBodyHandles) remove(id uint32) { h.m.Delete(id) }

func (r *Runtime) ensureFetchTemplates() *fetchTemplates {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fetchTemplates != nil {
		return r.fetchTemplates
	}
	if r.fetchBodies == nil {
		r.fetchBodies = &fetchBodyHandles{}
	}
	iso := r.iso
	respTmpl := v8.NewObjectTemplate(iso)
	respTmpl.SetInternalFieldCount(1)
	headersTmpl := v8.NewObjectTemplate(iso)
	r.fetchTemplates = &fetchTemplates{
		respObjTmpl: respTmpl,
		headersTmpl: headersTmpl,
		thrower: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			err, _ := v8.NewValue(iso, "fetch is not configured for this Context")
			iso.ThrowException(err)
			return nil
		}),
		fetchFn: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			c := r.contextFor(info.Context())
			args := info.Args()
			resolver, err := v8.NewPromiseResolver(info.Context())
			if err != nil {
				thrown, _ := v8.NewValue(iso, "fetch: cannot create promise")
				iso.ThrowException(thrown)
				return nil
			}
			if c == nil || c.fetcher == nil {
				rejectErr(iso, resolver, errors.New("fetch is not configured"))
				return resolver.GetPromise().Value
			}
			if len(args) == 0 {
				rejectErr(iso, resolver, errors.New("fetch: url required"))
				return resolver.GetPromise().Value
			}
			req, err := parseFetchArgs(info.Context(), args)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			if len(args) >= 2 && !args[1].IsNullOrUndefined() {
				if optsObj, err := args[1].AsObject(); err == nil {
					if sig, err := optsObj.Get("signal"); err == nil && sig.IsObject() {
						sigObj, _ := sig.AsObject()
						if ab, err := sigObj.Get("aborted"); err == nil && ab.Boolean() {
							rejectErr(iso, resolver, errors.New("AbortError: fetch aborted before send"))
							return resolver.GetPromise().Value
						}
					}
				}
			}
			if c.asyncFetch {
				c.asyncFetchPath(req, resolver)
				return resolver.GetPromise().Value
			}
			resp, err := c.fetcher(context.Background(), req)
			if err != nil {
				rejectErr(iso, resolver, err)
				return resolver.GetPromise().Value
			}
			respObj, err := buildResponseObject(c, info.Context(), resp)
			if err != nil {
				rejectErr(iso, resolver, fmt.Errorf("fetch: build response: %w", err))
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(respObj)
			return resolver.GetPromise().Value
		}),
		respText: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			body := bodyFromInfo(info, r.fetchBodies)
			resolver, _ := v8.NewPromiseResolver(info.Context())
			v, _ := v8.NewValue(iso, string(body))
			_ = resolver.Resolve(v)
			return resolver.GetPromise().Value
		}),
		respJSON: v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
			body := bodyFromInfo(info, r.fetchBodies)
			resolver, _ := v8.NewPromiseResolver(info.Context())
			val, err := v8.JSONParse(info.Context(), string(body))
			if err != nil {
				rejectErr(iso, resolver, fmt.Errorf("response.json: %w", err))
				return resolver.GetPromise().Value
			}
			_ = resolver.Resolve(val)
			return resolver.GetPromise().Value
		}),
	}
	return r.fetchTemplates
}

// bodyFromInfo reads the fetch body slab handle from info.This().GetInternalField(0)
// and returns the bytes (nil if missing).
func bodyFromInfo(info *v8.FunctionCallbackInfo, h *fetchBodyHandles) []byte {
	this := info.This()
	if this == nil {
		return nil
	}
	field := this.GetInternalField(0)
	if field == nil {
		return nil
	}
	id := uint32(field.Integer())
	if id == 0 {
		return nil
	}
	return h.get(id)
}

func installFetch(iso *v8.Isolate, v8ctx *v8.Context, c *Context) error {
	t := c.rt.ensureFetchTemplates()
	if c.fetcher == nil {
		// fetch() throws if invoked when no fetcher is configured.
		return v8ctx.Global().Set("fetch", t.thrower.GetFunction(v8ctx))
	}
	return v8ctx.Global().Set("fetch", t.fetchFn.GetFunction(v8ctx))
}

func parseFetchArgs(v8ctx *v8.Context, args []*v8.Value) (FetchRequest, error) {
	req := FetchRequest{
		URL:    args[0].String(),
		Method: "GET",
	}
	if req.URL == "" {
		return req, errors.New("fetch: url is empty")
	}
	if len(args) < 2 || args[1].IsNullOrUndefined() {
		return req, nil
	}
	optsObj, err := args[1].AsObject()
	if err != nil {
		return req, nil
	}
	if v, err := optsObj.Get("method"); err == nil && !v.IsNullOrUndefined() {
		req.Method = strings.ToUpper(v.String())
	}
	if v, err := optsObj.Get("body"); err == nil && !v.IsNullOrUndefined() {
		// Detect FormData via the `_pairs` internal property; serialize as
		// urlencoded and auto-set Content-Type when not provided. Real
		// browsers use multipart/form-data; for agent-side requests the
		// urlencoded form is what most servers happily accept.
		isFormData := false
		if v.IsObject() {
			if bObj, err := v.AsObject(); err == nil {
				if pairsV, err := bObj.Get("_pairs"); err == nil && pairsV.IsObject() {
					if tsVal, err := bObj.Get("toString"); err == nil {
						if tsFn, err := tsVal.AsFunction(); err == nil {
							if result, err := tsFn.Call(bObj); err == nil {
								req.Body = []byte(result.String())
								if req.Headers == nil {
									req.Headers = map[string]string{}
								}
								if _, has := req.Headers["Content-Type"]; !has {
									req.Headers["Content-Type"] = "application/x-www-form-urlencoded"
								}
								isFormData = true
							}
						}
					}
				}
			}
		}
		if !isFormData {
			req.Body = []byte(v.String())
		}
	}
	if v, err := optsObj.Get("headers"); err == nil && v.IsObject() {
		hObj, _ := v.AsObject()
		if hObj != nil {
			req.Headers = headerMapFromJS(v8ctx, hObj)
		}
	}
	return req, nil
}

// headerMapFromJS extracts header pairs from a JS object via JSON
// round-trip (Object.keys is not exposed by v8go on Object, so we
// stringify and re-parse).
func headerMapFromJS(v8ctx *v8.Context, obj *v8.Object) map[string]string {
	out := make(map[string]string)
	jsonGlobal, err := v8ctx.Global().Get("JSON")
	if err != nil {
		return out
	}
	jsonObj, err := jsonGlobal.AsObject()
	if err != nil {
		return out
	}
	stringifyVal, err := jsonObj.Get("stringify")
	if err != nil {
		return out
	}
	stringifyFn, err := stringifyVal.AsFunction()
	if err != nil {
		return out
	}
	jsoned, err := stringifyFn.Call(jsonObj, obj.Value)
	if err != nil {
		return out
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(jsoned.String()), &raw); err != nil {
		return out
	}
	for k, v := range raw {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// buildResponseObject creates the JS Response shape for a fetch result.
// Uses Runtime-cached templates; the body bytes are stashed in the
// fetchBodies slab and the handle goes in internal field 0 so the
// shared text/json callbacks can dispatch to the right body.
func buildResponseObject(c *Context, v8ctx *v8.Context, r *FetchResponse) (*v8.Value, error) {
	t := c.rt.ensureFetchTemplates()
	obj, err := t.respObjTmpl.NewInstance(v8ctx)
	if err != nil {
		return nil, err
	}
	bodyID := c.rt.fetchBodies.put(r.Body)
	c.fetchBodyIDs = append(c.fetchBodyIDs, bodyID)
	if err := obj.SetInternalField(0, int32(bodyID)); err != nil {
		return nil, err
	}
	_ = obj.Set("status", int32(r.Status))
	_ = obj.Set("ok", r.Status >= 200 && r.Status < 300)
	_ = obj.Set("statusText", r.StatusText)
	_ = obj.Set("url", r.URL)

	headersObj, err := t.headersTmpl.NewInstance(v8ctx)
	if err != nil {
		return nil, err
	}
	for k, vs := range r.Headers {
		_ = headersObj.Set(k, strings.Join(vs, ", "))
	}
	_ = obj.Set("headers", headersObj)
	_ = obj.Set("text", t.respText.GetFunction(v8ctx))
	_ = obj.Set("json", t.respJSON.GetFunction(v8ctx))
	return obj.Value, nil
}

func rejectErr(iso *v8.Isolate, resolver *v8.PromiseResolver, err error) {
	v, _ := v8.NewValue(iso, err.Error())
	_ = resolver.Reject(v)
}
