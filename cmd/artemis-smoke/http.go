package main

import (
	"context"
	"net/http"
)

// defaultClient is the HTTP client used for healthz polling.
var defaultClient = &http.Client{}

func newGetRequest(ctx context.Context, url string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return req, nil
}
