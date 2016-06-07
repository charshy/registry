package middleware

import (
	"fmt"
	"time"

	"github.com/amalgam8/registry/api/protocol"
	"github.com/amalgam8/registry/utils/logging"
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/rcrowley/go-metrics"
)

// MetricsMiddleware is an HTTP middleware that collects API usage metrics.
// It depends on the "ELAPSED_TIME" and "STATUS_CODE" being in r.Env (injected by rest.TimerMiddleware / rest.RecorderMiddleware),
// as well as the protocol.ProtocolKey and protocol.OperationKey values.
type MetricsMiddleware struct{}

// MiddlewareFunc implements the Middleware interface
func (mw *MetricsMiddleware) MiddlewareFunc(h rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		h(w, r)
		mw.collectMetrics(w, r)
	}
}

func (mw *MetricsMiddleware) collectMetrics(w rest.ResponseWriter, r *rest.Request) {
	proto, ok := r.Env[protocol.ProtocolKey].(protocol.Type)
	if !ok {
		return
	}

	operation, ok := r.Env[protocol.OperationKey].(protocol.Operation)
	if !ok {
		return
	}

	// Injected by TimerMiddleware
	latency, ok := r.Env["ELAPSED_TIME"].(*time.Duration)
	if !ok {
		logging.GetLogger(module).Error("could not find 'ELAPSED_TIME' parameter in HTTP request context")
		return
	}

	// Injected by RecorderMiddleware
	status, ok := r.Env["STATUS_CODE"].(int)
	if !ok {
		logging.GetLogger(module).Error("could not find 'STATUS_CODE' parameter in HTTP request context")
		return
	}

	histogramFactory := func() metrics.Histogram { return metrics.NewHistogram(metrics.NewExpDecaySample(256, 0.015)) }
	meterFactory := func() metrics.Meter { return metrics.NewMeter() }

	protocolName := protocol.NameOf(proto)
	operationName := operation.String()

	statusMeterName := fmt.Sprintf("api.%s.%s.status.%d", protocolName, operationName, status)
	statusMeter := metrics.DefaultRegistry.GetOrRegister(statusMeterName, meterFactory).(metrics.Meter)
	statusMeter.Mark(1)

	rateMeterName := fmt.Sprintf("api.%s.%s.rate", protocolName, operationName)
	rateMeter := metrics.DefaultRegistry.GetOrRegister(rateMeterName, meterFactory).(metrics.Meter)
	rateMeter.Mark(1)

	latencyHistogramName := fmt.Sprintf("api.%s.%s.latency", protocolName, operationName)
	latencyHistogram := metrics.DefaultRegistry.GetOrRegister(latencyHistogramName, histogramFactory).(metrics.Histogram)
	latencyHistogram.Update(int64(*latency))

	globalLatencyHistogramName := "api.global.latency"
	globalLatencyHistogram := metrics.DefaultRegistry.GetOrRegister(globalLatencyHistogramName, histogramFactory).(metrics.Histogram)
	globalLatencyHistogram.Update(int64(*latency))
}