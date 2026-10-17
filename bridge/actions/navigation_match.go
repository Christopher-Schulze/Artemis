package actions

import (
	"net/url"
	"strings"
)

func sameNavigationURL(requested, actual string) bool {
	expected, err := url.Parse(requested)
	if err != nil || expected.Scheme == "" {
		return false
	}
	observed, err := url.Parse(actual)
	if err != nil || observed.Scheme == "" {
		return false
	}
	if expected.User != nil || observed.User != nil || !strings.EqualFold(expected.Scheme, observed.Scheme) {
		return false
	}
	if expected.Opaque != "" || observed.Opaque != "" {
		return expected.String() == observed.String()
	}
	if expected.Scheme != "http" && expected.Scheme != "https" {
		return expected.String() == observed.String()
	}
	if expected.Hostname() == "" || observed.Hostname() == "" {
		return false
	}
	if !strings.EqualFold(expected.Hostname(), observed.Hostname()) || navigationPort(expected) != navigationPort(observed) {
		return false
	}
	return navigationPath(expected) == navigationPath(observed) && expected.RawQuery == observed.RawQuery && expected.ForceQuery == observed.ForceQuery && expected.EscapedFragment() == observed.EscapedFragment()
}

func navigationPort(value *url.URL) string {
	if port := value.Port(); port != "" {
		return port
	}
	if strings.EqualFold(value.Scheme, "https") {
		return "443"
	}
	return "80"
}

func navigationPath(value *url.URL) string {
	if value.Path == "" {
		return "/"
	}
	return value.ResolveReference(&url.URL{}).EscapedPath()
}
