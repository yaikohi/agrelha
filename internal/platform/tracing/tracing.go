package tracing

import (
	"context"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const ServiceName = "agrelha"

func noop(context.Context) error { return nil }

func Enabled() bool {
	return strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) != ""
}

func Init(ctx context.Context, version, commit string) (func(context.Context) error, error) {
	if !Enabled() {
		return noop, nil
	}

	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, err
	}

	attrs := []attribute.KeyValue{
		semconv.ServiceName(ServiceName),
		semconv.ServiceVersion(version),
		attribute.String("vcs.revision", commit),
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		attrs = append(attrs, semconv.HostName(host))
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL, attrs...),
	)
	if err != nil {
		return noop, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func Tracer(name string) trace.Tracer { return otel.Tracer("agrelha/" + name) }

func Job(ctx context.Context, name string, fn func(context.Context)) {
	ctx, span := Tracer("jobs").Start(ctx, "job."+name)
	defer span.End()
	fn(ctx)
}

func Starter(component string) func(context.Context, string) (context.Context, func(error)) {
	tr := Tracer(component)
	return func(ctx context.Context, name string) (context.Context, func(error)) {
		ctx, span := tr.Start(ctx, name)
		return ctx, func(err error) {
			if err != nil {
				span.RecordError(err)
			}
			span.End()
		}
	}
}
