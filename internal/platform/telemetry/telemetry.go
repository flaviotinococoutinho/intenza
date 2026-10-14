//intenza:paradigm imperative-shell

// Package telemetry wires OpenTelemetry: metrics exported in the Prometheus
// format (scraped from each binary's admin port) and traces sent over OTLP to
// a collector such as Jaeger. Trace context crosses the asynchronous hops
// too: it is stored with each outbox event and travels as the W3C
// traceparent header of the Kafka record.
package telemetry

import (
	"context"
	"errors"
	"net/http"
	"time"

	promclient "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/flaviotinococoutinho/intenza/internal/app"
	"github.com/flaviotinococoutinho/intenza/internal/platform/buildinfo"
)

// Telemetry holds the providers and the instruments every binary records into.
type Telemetry struct {
	Meter    metric.Meter
	Tracer   trace.Tracer
	Registry *promclient.Registry
	shutdown []func(context.Context) error

	HTTPRateLimited  metric.Int64Counter
	ConsumerEvents   metric.Int64Counter
	RelayPublished   metric.Int64Counter
	DeadlineLateness metric.Float64Histogram
	EscrowCalls      metric.Int64Counter
	EscrowDuration   metric.Float64Histogram
	BreakerState     metric.Int64Gauge
	MongoDuration    metric.Float64Histogram
	Business         metric.Int64Counter
}

// Setup builds the providers. Without an OTLP endpoint traces are not exported.
func Setup(ctx context.Context, service, otlpEndpoint string) (*Telemetry, error) {
	info := buildinfo.Read()
	res := resource.NewSchemaless(semconv.ServiceName(service), semconv.ServiceVersion(info.Version),
		attribute.String("vcs.revision", info.Commit))
	reg := promclient.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	exporter, err := prometheus.New(prometheus.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter), sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)
	t := &Telemetry{Registry: reg, shutdown: []func(context.Context) error{mp.Shutdown}}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if otlpEndpoint != "" {
		exp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(otlpEndpoint), otlptracegrpc.WithInsecure())
		if err != nil {
			return nil, err
		}
		tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(1))))
		otel.SetTracerProvider(tp)
		t.shutdown = append(t.shutdown, tp.Shutdown)
	}
	t.Meter = otel.Meter("intenza")
	t.Tracer = otel.Tracer("intenza")
	if err := t.instruments(); err != nil {
		return nil, err
	}
	app.TraceParent = TraceParent
	return t, nil
}

func (t *Telemetry) instruments() error {
	m := t.Meter
	var errs []error
	collect := func(err error) { errs = append(errs, err) }
	var err error
	latency := metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60)
	t.HTTPRateLimited, err = m.Int64Counter("intenza_ratelimit_rejected", metric.WithDescription("Requisições recusadas pelo limite (degraded=true quando o Redis falhou)"))
	collect(err)
	t.ConsumerEvents, err = m.Int64Counter("intenza_consumer_events", metric.WithDescription("Eventos consumidos por grupo e resultado"))
	collect(err)
	t.RelayPublished, err = m.Int64Counter("intenza_relay_published", metric.WithDescription("Eventos publicados pelo relay"))
	collect(err)
	t.DeadlineLateness, err = m.Float64Histogram("intenza_deadline_lateness", metric.WithUnit("s"), latency,
		metric.WithDescription("Atraso entre o prazo e o disparo, por origem (índice ou varredura)"))
	collect(err)
	t.EscrowCalls, err = m.Int64Counter("intenza_escrow_calls", metric.WithDescription("Chamadas ao agente de custódia por operação e resultado"))
	collect(err)
	t.EscrowDuration, err = m.Float64Histogram("intenza_escrow_call_duration", metric.WithUnit("s"), latency)
	collect(err)
	t.BreakerState, err = m.Int64Gauge("intenza_breaker_state", metric.WithDescription("0 fechado, 1 meio-aberto, 2 aberto"))
	collect(err)
	t.MongoDuration, err = m.Float64Histogram("intenza_mongo_command_duration", metric.WithUnit("s"), latency)
	collect(err)
	t.Business, err = m.Int64Counter("intenza_business_events", metric.WithDescription("Fatos de negócio por tipo"))
	collect(err)
	return errors.Join(errs...)
}

// Shutdown flushes and stops the providers.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	for _, f := range t.shutdown {
		errs = append(errs, f(ctx))
	}
	return errors.Join(errs...)
}

// MetricsHandler serves /metrics.
func (t *Telemetry) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(t.Registry, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// HTTP wraps a public handler with server spans and the standard
// http.server.request.duration histogram.
func (t *Telemetry) HTTP(h http.Handler, service string) http.Handler {
	return otelhttp.NewHandler(h, service, otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		return r.Method + " " + r.URL.Path
	}))
}

// TraceParent extracts the W3C traceparent of the span in ctx.
func TraceParent(ctx context.Context) string {
	c := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, c)
	return c["traceparent"]
}

// FromTraceParent continues a trace received from a message.
func FromTraceParent(ctx context.Context, tp string) context.Context {
	if tp == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": tp})
}

// Since is a convenience for recording durations in seconds.
func Since(start time.Time) float64 { return time.Since(start).Seconds() }

// Fact counts a business fact (app.Observer).
func (t *Telemetry) Fact(ctx context.Context, name string) {
	t.Business.Add(ctx, 1, metric.WithAttributes(attribute.String("fact", name)))
}

// MongoMonitor records the duration of every driver command.
func (t *Telemetry) MongoMonitor() *event.CommandMonitor {
	record := func(ctx context.Context, cmd string, d time.Duration, result string) {
		t.MongoDuration.Record(ctx, d.Seconds(), metric.WithAttributes(attribute.String("command", cmd), attribute.String("result", result)))
	}
	return &event.CommandMonitor{
		Succeeded: func(ctx context.Context, e *event.CommandSucceededEvent) {
			record(ctx, e.CommandName, e.Duration, "ok")
		},
		Failed: func(ctx context.Context, e *event.CommandFailedEvent) {
			record(ctx, e.CommandName, e.Duration, "error")
		},
	}
}

// Attrs builds measurement attributes from key/value pairs.
func Attrs(kv ...string) metric.MeasurementOption {
	attrs := make([]attribute.KeyValue, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		attrs = append(attrs, attribute.String(kv[i], kv[i+1]))
	}
	return metric.WithAttributes(attrs...)
}
