package httpclient

import (
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func Wrap(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

func Transport() http.RoundTripper { return Wrap(nil) }

func New(timeout time.Duration) *http.Client {
	return &http.Client{Transport: Transport(), Timeout: timeout}
}
