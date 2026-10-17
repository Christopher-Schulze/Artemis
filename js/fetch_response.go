package js

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"unicode/utf8"

	v8 "github.com/tommie/v8go"
	"golang.org/x/net/http/httpguts"

	"github.com/Christopher-Schulze/Artemis/network"
)

const maxFetchBodyHandles = 4096
const maxFetchHeaderFields = 1024
const fetchBodyBrand = "artemis.fetch.body.v1"

// Payloads are immutable and shared by clones. State and reference transitions
// occur only under the Runtime isolate gate.
type fetchPayload struct {
	data       []byte
	references int
}

type fetchBody struct {
	id      uint32
	owner   *Context
	payload *fetchPayload
	used    bool
	null    bool
	signal  *fetchSignalWatch
}

func (body *fetchBody) releaseSignal() {
	if body.signal == nil {
		return
	}
	signal := body.signal
	body.signal = nil
	signal.retire(body.owner.closed.Load())
}

func (body *fetchBody) releasePayload() {
	if body.payload == nil {
		return
	}
	body.payload.references--
	if body.payload.references == 0 {
		body.owner.fetchBodyBytes -= int64(len(body.payload.data))
		body.payload.data = nil
	}
	body.payload = nil
}

func (c *Context) newResponseBody(data []byte, null bool) (*fetchBody, error) {
	limit := network.DefaultPolicyConfig().MaxResponseBodyBytes
	if c.policy != nil {
		limit = c.policy.Config().MaxResponseBodyBytes
	}
	if int64(len(data)) > limit-c.fetchBodyBytes {
		return nil, errors.New("fetch: retained response body byte limit exceeded")
	}
	if null && len(data) != 0 {
		return nil, errors.New("fetch: null-body response contains bytes")
	}
	body := &fetchBody{owner: c, null: null}
	if !null {
		body.payload = &fetchPayload{data: bytes.Clone(data), references: 1}
		c.fetchBodyBytes += int64(len(data))
	}
	return body, nil
}

func (r *Runtime) responseBody(info *v8.FunctionCallbackInfo) (*fetchBody, error) {
	owner := r.contextFor(info.Context())
	this := info.This()
	if owner == nil || owner.closed.Load() || this == nil || this.InternalFieldCount() != 2 {
		return nil, errors.New("TypeError: invalid response receiver")
	}
	brand := this.GetInternalField(1)
	defer brand.Release()
	if !brand.IsString() || brand.String() != fetchBodyBrand {
		return nil, errors.New("TypeError: invalid response receiver")
	}
	field := this.GetInternalField(0)
	defer field.Release()
	id, valid := checkedInt64ToUint32(field.Integer())
	if !valid || id == 0 {
		return nil, errors.New("TypeError: invalid response body handle")
	}
	body := r.fetchBodies.get(id)
	if body == nil || body.owner != owner {
		return nil, errors.New("TypeError: foreign or retired response body")
	}
	return body, nil
}

func (c *Context) newResponseBodyObject(body *fetchBody) (result *v8.Value, returnErr error) {
	if body.signal != nil {
		body.signal.references++
	}
	defer func() {
		if returnErr != nil {
			body.releasePayload()
			body.releaseSignal()
		}
	}()
	if len(c.fetchBodyIDs) >= maxFetchBodyHandles {
		return nil, errors.New("fetch: response body handle limit exceeded")
	}
	templates, err := c.rt.ensureFetchTemplates()
	if err != nil {
		return nil, err
	}
	object, err := templates.respObjTmpl.NewInstance(c.v8ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if returnErr != nil {
			object.Release()
			c.rt.fetchBodies.remove(body.id)
		}
	}()
	body.id, err = c.rt.fetchBodies.put(body)
	if err != nil {
		return nil, err
	}
	if err := object.SetInternalField(0, int32(body.id)); err != nil {
		return nil, err
	}
	if err := object.SetInternalField(1, fetchBodyBrand); err != nil {
		return nil, err
	}
	c.fetchBodyIDs = append(c.fetchBodyIDs, body.id)
	if body.signal != nil {
		body.signal.bodyIDs = append(body.signal.bodyIDs, body.id)
	}
	return object.Value, nil
}

func (r *Runtime) consumeResponseBody(info *v8.FunctionCallbackInfo, binary bool) *v8.Value {
	defer info.Release()
	owner := r.contextFor(info.Context())
	if owner == nil {
		return r.responseError(info, errors.New("response: active owner required"))
	}
	resolver, err := v8.NewPromiseResolver(info.Context())
	if err != nil {
		return r.responseError(info, err)
	}
	defer resolver.Release()
	body, err := r.responseBody(info)
	if err == nil && body.used {
		err = errors.New("TypeError: response body is already consumed")
	}
	if err != nil {
		owner.rejectBodyError(resolver, err)
		return owner.callbackResult(resolver.GetPromise().Value)
	}
	if aborted, err := body.signal.rejectAborted(resolver); aborted || err != nil {
		if err != nil {
			owner.rejectBodyError(resolver, err)
		}
		return owner.callbackResult(resolver.GetPromise().Value)
	}
	var data []byte
	if body.payload != nil {
		data = body.payload.data
	}
	if !body.null {
		body.used = true
	}
	defer body.releasePayload()
	defer body.releaseSignal()
	var value *v8.Value
	if binary {
		value, err = info.Context().NewArrayBuffer(data)
	} else {
		value, err = v8.NewValue(r.iso, string(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	}
	if err != nil {
		owner.rejectBodyError(resolver, err)
	} else {
		defer value.Release()
		if !resolver.Resolve(value) {
			owner.rt.execution.recordFailure(errors.New("response: native promise resolution failed"))
		}
	}
	return owner.callbackResult(resolver.GetPromise().Value)
}

func (c *Context) rejectBodyError(resolver *v8.PromiseResolver, cause error) {
	value, err := c.v8ctx.NewTypeError(cause.Error())
	if err == nil {
		defer value.Release()
		if !resolver.Reject(value) {
			err = errors.New("response: native rejection failed")
		}
	}
	if err != nil {
		if span := c.rt.execution; span != nil {
			span.recordFailure(err)
		} else {
			thrown := c.rt.iso.ThrowException(v8.Null(c.rt.iso))
			thrown.Release()
		}
	}
}

func (r *Runtime) responseError(info *v8.FunctionCallbackInfo, cause error) *v8.Value {
	value, err := v8.NewValue(r.iso, cause.Error())
	if err != nil {
		value = v8.Null(r.iso)
	} else {
		defer value.Release()
	}
	thrown := r.iso.ThrowException(value)
	if owner := r.contextFor(info.Context()); owner != nil {
		return owner.callbackResult(thrown)
	}
	return thrown
}

type fetchResponseMetadata struct {
	Status     int         `json:"status"`
	StatusText string      `json:"statusText"`
	URL        string      `json:"url"`
	Headers    [][2]string `json:"headers"`
}

func responseMetadata(response *FetchResponse) ([]byte, error) {
	if response == nil {
		return nil, errors.New("fetch: nil response")
	}
	if response.Status < 200 || response.Status > 599 {
		return nil, fmt.Errorf("fetch: invalid response status %d", response.Status)
	}
	if !httpguts.ValidHeaderFieldValue(response.StatusText) || !utf8.ValidString(response.URL) || strings.ContainsAny(response.URL, "\x00\r\n") {
		return nil, errors.New("fetch: invalid response metadata")
	}
	size := len(response.StatusText) + len(response.URL)
	if size > http.DefaultMaxHeaderBytes || len(response.Headers) > maxFetchHeaderFields {
		return nil, errors.New("fetch: response metadata limit exceeded")
	}
	metadata := fetchResponseMetadata{Status: response.Status, StatusText: headerLatin1(response.StatusText), URL: response.URL}
	names := make([]string, 0, len(response.Headers))
	for name := range response.Headers {
		names = append(names, name)
	}
	sort.Strings(names)
	fields := 0
	for _, name := range names {
		if !httpguts.ValidHeaderFieldName(name) {
			return nil, errors.New("fetch: invalid response header name")
		}
		for _, value := range response.Headers[name] {
			fields++
			size += len(name) + len(value)
			if size > http.DefaultMaxHeaderBytes || fields > maxFetchHeaderFields || !httpguts.ValidHeaderFieldValue(value) {
				return nil, errors.New("fetch: invalid or excessive response header value")
			}
			if strings.EqualFold(name, "Set-Cookie") || strings.EqualFold(name, "Set-Cookie2") {
				continue
			}
			metadata.Headers = append(metadata.Headers, [2]string{name, headerLatin1(value)})
		}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if len(encoded) > http.DefaultMaxHeaderBytes {
		return nil, errors.New("fetch: encoded response metadata limit exceeded")
	}
	return encoded, nil
}

func headerLatin1(value string) string {
	var output strings.Builder
	output.Grow(len(value))
	for index := range len(value) {
		output.WriteRune(rune(value[index]))
	}
	return output.String()
}

func (c *Context) bindResponseFactory() error {
	global := c.v8ctx.Global()
	defer global.Release()
	value, err := global.Get("__artemisResponse")
	if err != nil {
		return err
	}
	c.responseFactory, err = value.AsFunction()
	if err != nil {
		value.Release()
		return err
	}
	encoder, err := global.Get("__artemisEncodeRequestHeaders")
	if err != nil {
		return err
	}
	c.requestHeaderEncoder, err = encoder.AsFunction()
	if err != nil {
		encoder.Release()
		return err
	}
	c.responseTrue, err = v8.NewValue(c.rt.iso, true)
	if err != nil {
		return err
	}
	c.responseFalse, err = v8.NewValue(c.rt.iso, false)
	if err != nil {
		return err
	}
	signal, err := global.Get("__artemisBindFetchSignal")
	if err != nil {
		return err
	}
	c.fetchSignalFactory, err = signal.AsFunction()
	if err != nil {
		signal.Release()
	}
	return err
}

const fetchResponseBootstrap = `
(() => {
  __artemisConfigureAbort(setTimeout, clearTimeout, __artemisEventTarget);
  const watchSignal = __artemisWatchFetchSignal, cancelFetch = __fetch_abort;
  Object.defineProperty(globalThis, '__artemisBindFetchSignal', {value: (signal, id) => watchSignal(signal, id, cancelFetch)});
  const HeaderList = Headers, ByteArray = Uint8Array, immutableHeaders = __artemisFreezeHeaders;
  const parse = JSON.parse, apply = Reflect.apply, define = Object.defineProperties;
  const failure = error => { throw new TypeError(String(error)); };
  const consume = (method, receiver) => {
    try { return apply(method, receiver, []); }
    catch (error) { return Promise.reject(new TypeError(String(error))); }
  };
  function decorate(native, metadata) {
    const used = native.__bodyUsed, clone = native.__clone;
    const text = native.text, buffer = native.arrayBuffer;
    const headers = immutableHeaders(new HeaderList(metadata.headers || []));
    delete native.__bodyUsed; delete native.__clone; delete native.__discard;
    define(native, {
      status: {value: metadata.status, enumerable: true},
      statusText: {value: metadata.statusText, enumerable: true},
      url: {value: metadata.url, enumerable: true},
      ok: {value: metadata.status >= 200 && metadata.status < 300, enumerable: true},
      headers: {value: headers, enumerable: true},
      bodyUsed: {get() { try { return apply(used, this, []); } catch (error) { return failure(error); } }, enumerable: true},
      text: {value() { return consume(text, this); }},
      json: {value() { return consume(text, this).then(parse); }},
      arrayBuffer: {value() { return consume(buffer, this); }},
      bytes: {value() { return consume(buffer, this).then(value => new ByteArray(value)); }},
      clone: {value() {
        let copy;
        try { copy = apply(clone, this, []); } catch (error) { return failure(error); }
        return wrap(copy, {status: metadata.status, statusText: metadata.statusText, url: metadata.url, headers});
      }},
    });
    return native;
  }
  function wrap(native, metadata) {
    const discard = native.__discard;
    try { return decorate(native, metadata); }
    catch (error) { apply(discard, native, []); throw error; }
  }
  Object.defineProperty(globalThis, '__artemisResponse', {value: wrap});
})();
`
