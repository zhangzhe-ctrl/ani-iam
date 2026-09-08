package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	kratosmetrics "github.com/go-kratos/kratos/contrib/otel/v3/metrics"
	kratostracing "github.com/go-kratos/kratos/contrib/otel/v3/tracing"
	"github.com/go-kratos/kratos/v3/middleware"
	"github.com/go-kratos/kratos/v3/middleware/logging"
	"github.com/go-kratos/kratos/v3/middleware/metadata"
	"github.com/go-kratos/kratos/v3/middleware/recovery"
	"github.com/go-kratos/kratos/v3/middleware/validate"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	metricSdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	traceSdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"

	"github.com/zhangzhe-ctrl/ani-iam/internal/biz"
)

type Observability struct {
	registry                       *prometheus.Registry
	meterProvider                  *metricSdk.MeterProvider
	tracerProvider                 *traceSdk.TracerProvider
	requests                       metric.Int64Counter
	seconds                        metric.Float64Histogram
	apiKeyStaleNonExpiring         atomic.Int64
	apiKeyUnusualServicePrincipals atomic.Int64
	apiKeySnapshotTimestamp        atomic.Int64
}

func NewObservability(name, version string, readiness *Readiness) (*Observability, error) {
	if err := kratosmetrics.EnableOTELExemplar(); err != nil {
		return nil, fmt.Errorf("enable OpenTelemetry exemplars: %w", err)
	}

	registry := prometheus.NewRegistry()
	if err := registry.Register(prometheus.NewGoCollector()); err != nil {
		return nil, fmt.Errorf("register Go collector: %w", err)
	}
	if err := registry.Register(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{})); err != nil {
		return nil, fmt.Errorf("register process collector: %w", err)
	}
	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registry),
		otelprometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry Prometheus exporter: %w", err)
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(name),
		semconv.ServiceVersion(version),
	)
	meterProvider := metricSdk.NewMeterProvider(
		metricSdk.WithResource(res),
		metricSdk.WithReader(exporter),
		metricSdk.WithView(kratosmetrics.DefaultSecondsHistogramView(kratosmetrics.DefaultServerSecondsHistogramName)),
	)
	tracerProvider := traceSdk.NewTracerProvider(
		traceSdk.WithResource(res),
		traceSdk.WithSampler(traceSdk.ParentBased(traceSdk.AlwaysSample())),
	)

	meter := meterProvider.Meter(name)
	requests, err := kratosmetrics.DefaultRequestsCounter(meter, kratosmetrics.DefaultServerRequestsCounterName)
	if err != nil {
		return nil, fmt.Errorf("create Kratos server request counter: %w", err)
	}
	seconds, err := kratosmetrics.DefaultSecondsHistogram(meter, kratosmetrics.DefaultServerSecondsHistogramName)
	if err != nil {
		return nil, fmt.Errorf("create Kratos server latency histogram: %w", err)
	}
	observability := &Observability{
		registry:       registry,
		meterProvider:  meterProvider,
		tracerProvider: tracerProvider,
		requests:       requests,
		seconds:        seconds,
	}
	if _, err := meter.Int64ObservableGauge(
		"ani_iam_runtime_ready",
		metric.WithDescription("Whether the isolated CP0 runtime is ready to receive traffic."),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			if readiness.Ready() {
				observer.Observe(1)
			} else {
				observer.Observe(0)
			}
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("create readiness gauge: %w", err)
	}
	if _, err := meter.Int64ObservableGauge(
		"ani_iam_api_key_stale_non_expiring_count",
		metric.WithDescription("Number of active non-expiring API keys not used within the stale threshold."),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(observability.apiKeyStaleNonExpiring.Load())
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("create stale non-expiring API key gauge: %w", err)
	}
	if _, err := meter.Int64ObservableGauge(
		"ani_iam_service_principal_unusual_active_api_keys_count",
		metric.WithDescription("Number of service principals with an unusual active API key count."),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(observability.apiKeyUnusualServicePrincipals.Load())
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("create unusual active API key gauge: %w", err)
	}
	if _, err := meter.Int64ObservableGauge(
		"ani_iam_api_key_operational_snapshot_timestamp_seconds",
		metric.WithDescription("Unix timestamp of the latest successful API key operational snapshot; zero means unavailable."),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			observer.Observe(observability.apiKeySnapshotTimestamp.Load())
			return nil
		}),
	); err != nil {
		return nil, fmt.Errorf("create API key operational snapshot timestamp gauge: %w", err)
	}
	otel.SetMeterProvider(meterProvider)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return observability, nil
}

func (o *Observability) SetAPIKeyOperationalSnapshot(snapshot biz.APIKeyOperationalSnapshot) error {
	if snapshot.StaleNonExpiringCount < 0 || snapshot.UnusualServicePrincipalCount < 0 || snapshot.ObservedAt.IsZero() {
		return errors.New("API key operational snapshot is invalid")
	}
	o.apiKeyStaleNonExpiring.Store(snapshot.StaleNonExpiringCount)
	o.apiKeyUnusualServicePrincipals.Store(snapshot.UnusualServicePrincipalCount)
	o.apiKeySnapshotTimestamp.Store(snapshot.ObservedAt.UTC().Unix())
	return nil
}

func (o *Observability) Gatherer() prometheus.Gatherer {
	return o.registry
}

func (o *Observability) ServerMiddleware(logger *slog.Logger) []middleware.Middleware {
	return []middleware.Middleware{
		recovery.Recovery(recovery.WithLogger(logger)),
		metadata.Server(),
		kratostracing.Server(kratostracing.WithTracerProvider(o.tracerProvider)),
		logging.Server(logger),
		kratosmetrics.Server(
			kratosmetrics.WithRequests(o.requests),
			kratosmetrics.WithSeconds(o.seconds),
		),
		validate.Validator(),
	}
}

func (o *Observability) Shutdown(ctx context.Context) error {
	return errors.Join(
		o.meterProvider.Shutdown(ctx),
		o.tracerProvider.Shutdown(ctx),
	)
}
