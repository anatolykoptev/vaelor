package filewatcher

import "context"

// OTelSpan is a minimal interface representing an OpenTelemetry span.
// It abstracts over the actual OTel API so this library does not need
// to depend on go.opentelemetry.io/otel.
//
// Implementations should call End() when the span is no longer needed.
//
// The real go.opentelemetry.io/otel/trace.Span satisfies this interface
// (End, SetStatus, SetAttributes, RecordError). Users can wrap their
// span type with a small adapter:
//
//	func toOtelSpan(s trace.Span) filewatcher.OTelSpan {
//	    return &otelSpanAdapter{s: s}
//	}
type OTelSpan interface {
	// End completes the span. Should be called exactly once.
	End()
	// SetStatus sets the span status (typically "ok" or "error").
	SetStatus(code, description string)
	// SetAttributes attaches string key-value pairs to the span.
	SetAttributes(attrs ...Attribute)
}

// Attribute is a minimal key-value pair for span attributes.
type Attribute struct {
	Key   string
	Value string
}

// OTelMiddleware returns a Middleware that wraps each event in a span.
// The span is created via the provided startSpan function and ended when
// the downstream handler returns.
//
// startSpan receives the event path and operation string. The returned
// span will have its status set to "ok" on success or "error" on
// failure, and a "filewatcher.error" attribute on error.
//
// The function is nil-safe: if startSpan is nil, the middleware is a
// no-op (events pass through unchanged).
func OTelMiddleware(startSpan func(path, op string) OTelSpan) Middleware {
	if startSpan == nil {
		return func(next Handler) Handler {
			return func(ctx context.Context, event Event) error {
				return next(ctx, event)
			}
		}
	}

	return func(next Handler) Handler {
		return func(ctx context.Context, event Event) error {
			span := startSpan(event.Path, event.Op.String())
			if span == nil {
				return next(ctx, event)
			}

			defer span.End()

			span.SetAttributes(
				Attribute{Key: "filewatcher.path", Value: event.Path},
				Attribute{Key: "filewatcher.op", Value: event.Op.String()},
				Attribute{Key: "filewatcher.is_dir", Value: boolToString(event.IsDir)},
			)

			err := next(ctx, event)
			if err != nil {
				span.SetStatus("error", err.Error())
				span.SetAttributes(Attribute{Key: "filewatcher.error", Value: err.Error()})
			} else {
				span.SetStatus("ok", "")
			}

			return err
		}
	}
}

func boolToString(b bool) string {
	if b {
		return "true"
	}

	return "false"
}
