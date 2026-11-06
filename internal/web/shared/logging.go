package shared

import (
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/utils"
	"go.opentelemetry.io/otel/trace"

	"agrelha/internal/web/metrics"
)

var reqCounter atomic.Uint64

func Unobserved(path string) bool {
	return path == "/healthz" ||
		path == "/metrics" ||
		strings.HasPrefix(path, "/assets")
}

func Untraced(path string) bool {
	return Unobserved(path) || path == "/sse"
}

func RequestLogger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		path := c.Path()
		if Unobserved(path) {
			return c.Next()
		}

		rid := strconv.FormatUint(reqCounter.Add(1), 10)
		c.Locals("rid", rid)
		start := time.Now()

		err := c.Next()

		dur := time.Since(start)
		status := c.Response().StatusCode()
		ds := c.Get("Datastar-Request") != ""
		method := utils.CopyString(c.Method())

		route := c.Route().Path
		metrics.HTTPRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
		metrics.HTTPDuration.WithLabelValues(method, route).Observe(dur.Seconds())
		if ds {
			metrics.DatastarRequests.Inc()
		}

		lg := slog.With(
			"rid", rid,
			"method", method,
			"path", path,
			"status", status,
			"dur_ms", dur.Milliseconds(),
			"ip", c.IP(),
			"ds", ds,
			"accept", c.Get("Accept"),
			"resp_ct", string(c.Response().Header.ContentType()),
		)
		if tid := traceID(c); tid != "" {
			lg = lg.With("trace_id", tid)
		}
		if loc := c.Response().Header.Peek("Location"); len(loc) > 0 {
			lg = lg.With("location", string(loc))
		}
		if actor, ok := c.Locals("actor").(string); ok && actor != "" {
			lg = lg.With("actor", actor)
		}

		switch {
		case status >= 500:
			lg.Error("request")
		case status >= 400:
			lg.Warn("request")
		default:
			lg.Info("request")
		}
		return err
	}
}

func RequestID(c *fiber.Ctx) string {
	if v, ok := c.Locals("rid").(string); ok {
		return v
	}
	return "-"
}

func traceID(c *fiber.Ctx) string {
	sc := trace.SpanContextFromContext(c.UserContext())
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
