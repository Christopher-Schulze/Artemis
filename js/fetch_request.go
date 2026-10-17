package js

import (
	"errors"
	"math"
	"strings"

	"github.com/Christopher-Schulze/Artemis/network"
	v8 "github.com/tommie/v8go"
	"golang.org/x/net/http/httpguts"
)

func parseFetchMethod(options *v8.Object, request *FetchRequest) error {
	value, err := options.Get("method")
	if err != nil {
		return err
	}
	defer value.Release()
	if value.IsUndefined() {
		return nil
	}
	method, err := value.StringChecked()
	if err != nil {
		return err
	}
	if !httpguts.ValidHeaderFieldName(method) {
		return errors.New("fetch: invalid method")
	}
	upper := strings.ToUpper(method)
	switch upper {
	case "CONNECT", "TRACE", "TRACK":
		return errors.New("fetch: forbidden method")
	case "DELETE", "GET", "HEAD", "OPTIONS", "POST", "PUT":
		method = upper
	}
	request.Method = method
	return nil
}

func (c *Context) parseFetchHeaders(options *v8.Object, request *FetchRequest) error {
	value, err := options.Get("headers")
	if err != nil {
		return err
	}
	defer value.Release()
	if value.IsUndefined() {
		return nil
	}
	object, err := value.AsObject()
	if err != nil {
		return errors.New("fetch: invalid headers")
	}
	request.Headers, err = c.headerMapFromJS(object)
	return err
}

func (c *Context) parseFetchBody(options *v8.Object, request *FetchRequest) error {
	value, err := options.Get("body")
	if err != nil {
		return err
	}
	defer value.Release()
	if value.IsNullOrUndefined() {
		return nil
	}
	if request.Method == "GET" || request.Method == "HEAD" {
		return errors.New("fetch: GET/HEAD cannot have a body")
	}
	limit := network.DefaultPolicyConfig().MaxRequestBodyBytes
	if c.policy != nil {
		limit = c.policy.Config().MaxRequestBodyBytes
	}
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}
	if value.IsArrayBuffer() || value.IsArrayBufferView() || value.IsSharedArrayBuffer() {
		request.Body, err = value.BufferBytes(int(limit))
		return err
	}
	text, err := value.StringChecked()
	if err != nil {
		return err
	}
	if int64(len(text)) > limit {
		return errors.New("fetch: request body exceeds limit")
	}
	request.Body = []byte(text)
	return addCompatibleFormContentType(value, request)
}

// Preserve the existing urlencoded FormData compatibility surface. Multipart
// FormData remains outside this binding's browser-equivalence claim.
func addCompatibleFormContentType(value *v8.Value, request *FetchRequest) error {
	if !value.IsObject() || hasHeader(request.Headers, "Content-Type") {
		return nil
	}
	object, err := value.AsObject()
	if err != nil {
		return err
	}
	pairs, err := object.Get("_pairs")
	if err != nil {
		return err
	}
	defer pairs.Release()
	if !pairs.IsObject() {
		return nil
	}
	if request.Headers == nil {
		request.Headers = make(map[string]string)
	}
	request.Headers["Content-Type"] = "application/x-www-form-urlencoded"
	return nil
}

// Fetch owns these wire fields; script-supplied values cannot replace transport,
// credential or origin authority. A standalone Headers object has no such guard.
func forbiddenFetchHeader(name, value string) bool {
	name = strings.ToLower(name)
	if strings.HasPrefix(name, "proxy-") || strings.HasPrefix(name, "sec-") {
		return true
	}
	switch name {
	case "accept-charset", "accept-encoding", "access-control-request-headers", "access-control-request-method",
		"connection", "content-length", "cookie", "cookie2", "date", "dnt", "expect", "host", "keep-alive",
		"origin", "referer", "set-cookie", "te", "trailer", "transfer-encoding", "upgrade", "via":
		return true
	case "x-http-method", "x-http-method-override", "x-method-override":
		return containsForbiddenMethod(value)
	}
	return false
}

func containsForbiddenMethod(value string) bool {
	start, quoted, escaped := 0, false, false
	for index := 0; index <= len(value); index++ {
		if index == len(value) || value[index] == ',' && !quoted {
			method := strings.Trim(value[start:index], "\t ")
			if strings.EqualFold(method, "CONNECT") || strings.EqualFold(method, "TRACE") || strings.EqualFold(method, "TRACK") {
				return true
			}
			start = index + 1
			continue
		}
		if escaped {
			escaped = false
			continue
		}
		if value[index] == '\\' && quoted {
			escaped = true
			continue
		}
		if value[index] == '"' {
			quoted = !quoted
		}
	}
	return false
}
