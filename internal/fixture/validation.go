package fixture

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"strings"
	"unicode/utf8"
)

const (
	maxRegisteredScenarios             = 1024
	maxScenarioBodyBytes               = 8 << 20
	maxScenarioMetadataBytes           = 32 << 10
	maxRegisteredBytes           int64 = 64 << 20
	maxFixtureConcurrentRequests       = 8
)

func prepareScenario(sc Scenario) (Scenario, int64, error) {
	if sc.inertReplay && (sc.Handler != nil || sc.RunScripts || sc.AsyncFetch || sc.WaitForIdle || sc.ScriptCount != 0 || sc.Expect.Eval != "") {
		return Scenario{}, 0, fmt.Errorf("fixture: inert replay projection cannot enable execution")
	}
	if sc.ID == "" || len(sc.ID) > 256 || !utf8.ValidString(sc.ID) || strings.ContainsAny(sc.ID, " \t\r\n\x00") {
		return Scenario{}, 0, fmt.Errorf("fixture: bounded nonempty scenario ID required")
	}
	if sc.Path == "" {
		sc.Path = "/" + sc.ID
	}
	if !strings.HasPrefix(sc.Path, "/") {
		sc.Path = "/" + sc.Path
	}
	parsed, err := url.ParseRequestURI(sc.Path)
	if err != nil || len(sc.Path) > 1024 || sc.Path == "/" || parsed == nil || parsed.Scheme != "" || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" || strings.ContainsAny(sc.Path, "{}# \t\r\n\x00") {
		return Scenario{}, 0, fmt.Errorf("fixture: invalid scenario route %q", sc.Path)
	}
	canonical := &url.URL{Path: parsed.Path}
	if canonical.EscapedPath() != sc.Path || path.Clean(parsed.Path) != strings.TrimSuffix(parsed.Path, "/") || strings.ContainsAny(parsed.Path, "{}\\\x00\r\n") {
		return Scenario{}, 0, fmt.Errorf("fixture: noncanonical scenario route %q", sc.Path)
	}
	if !validScenarioKind(sc.Kind) || sc.Status < 0 || (sc.Status != 0 && (sc.Status < 100 || sc.Status > 599)) || sc.BodyBytes < 0 || sc.BodyBytes > maxScenarioBodyBytes || sc.ScriptCount < 0 || sc.ScriptCount > 4096 {
		return Scenario{}, 0, fmt.Errorf("fixture %q: invalid kind, status or size/count", sc.ID)
	}
	if sc.ContentType != "" {
		if _, _, err := mime.ParseMediaType(sc.ContentType); err != nil {
			return Scenario{}, 0, fmt.Errorf("fixture %q content type: %w", sc.ID, err)
		}
	}
	if sc.Handler != nil && nilScenarioHandler(sc.Handler) {
		return Scenario{}, 0, fmt.Errorf("fixture %q: nil handler value", sc.ID)
	}
	if err := sc.Expect.Validate(); err != nil {
		return Scenario{}, 0, err
	}
	metadata, err := scenarioMetadataBytes(sc)
	if err != nil {
		return Scenario{}, 0, err
	}
	if len(sc.HTML) > maxScenarioBodyBytes {
		return Scenario{}, 0, fmt.Errorf("fixture %q: inline body byte limit exceeded", sc.ID)
	}
	return cloneScenario(sc), metadata + int64(len(sc.HTML)), nil
}

func validScenarioKind(kind Kind) bool {
	switch kind {
	case KindHTML, KindCSS, KindJS, KindSPA, KindNavigation, KindRedirect, KindCookie, KindStorage, KindForm, KindFile, KindFrame, KindShadowDOM, KindDialog, KindCanvas, KindWebSocket, KindServiceWorker, KindAuth, KindChallenge, KindLarge, KindMalformed, KindSlow, KindCrash, KindUnknown:
		return true
	default:
		return false
	}
}

func nilScenarioHandler(handler http.Handler) bool {
	value := reflect.ValueOf(handler)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func scenarioMetadataBytes(sc Scenario) (int64, error) {
	if len(sc.Expect.Contains) > 128 || len(sc.Expect.NotContains) > 128 {
		return 0, fmt.Errorf("fixture %q: expectation count limit exceeded", sc.ID)
	}
	fields := []string{sc.ID, sc.Path, string(sc.Kind), sc.Description, sc.ContentType, sc.Expect.Eval, sc.Expect.EvalContains, sc.Expect.URL}
	fields = append(fields, sc.Expect.NotContains...)
	var size int64
	for _, field := range fields {
		if len(field) > 16<<10 || !utf8.ValidString(field) {
			return 0, fmt.Errorf("fixture %q: invalid or oversized metadata", sc.ID)
		}
		size += int64(len(field))
	}
	if size > maxScenarioMetadataBytes {
		return 0, fmt.Errorf("fixture %q: aggregate metadata byte limit exceeded", sc.ID)
	}
	fieldLimit, totalLimit := 16<<10, int64(maxScenarioMetadataBytes)
	if sc.inertReplay {
		// Snapshot title/full-text assertions are bounded captured data, not
		// ordinary fixture metadata. Keep them intact and count every byte.
		fieldLimit, totalLimit = maxReplayTextBytes, size+maxReplayTextBytes
	}
	for _, field := range append([]string{sc.Expect.Title}, sc.Expect.Contains...) {
		if len(field) > fieldLimit || !utf8.ValidString(field) {
			return 0, fmt.Errorf("fixture %q: invalid or oversized expectation", sc.ID)
		}
		size += int64(len(field))
		if size > totalLimit {
			return 0, fmt.Errorf("fixture %q: aggregate assertion byte limit exceeded", sc.ID)
		}
	}
	return size, nil
}

func cloneScenario(sc Scenario) Scenario {
	if sc.Expect.Contains != nil {
		sc.Expect.Contains = append([]string{}, sc.Expect.Contains...)
	}
	if sc.Expect.NotContains != nil {
		sc.Expect.NotContains = append([]string{}, sc.Expect.NotContains...)
	}
	return sc
}
