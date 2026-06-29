package telemetry

import (
	"context"
	"fmt"
	"math"
	"time"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// OtelCore is a zapcore.Core that forwards log records to the OTel log SDK,
// which exports them to OpenObserve. Intended to be tee'd with the stderr core
// so logs flow to both destinations.
type OtelCore struct {
	logger   log.Logger
	fields   []log.KeyValue
	minLevel zapcore.Level
}

func newOtelCore(name string, minLevel zapcore.Level) *OtelCore {
	return &OtelCore{
		logger:   global.GetLoggerProvider().Logger(name),
		minLevel: minLevel,
	}
}

// BridgeLogger returns a new *zap.Logger that tees every record to both the
// original logger's core (stderr) and the OTel log pipeline (OpenObserve).
// Call this after telemetry.Setup so the global LoggerProvider is populated.
func BridgeLogger(base *zap.Logger, serviceName string) *zap.Logger {
	otelCore := newOtelCore(serviceName, zapcore.InfoLevel)
	tee := zapcore.NewTee(base.Core(), otelCore)
	return zap.New(tee, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))
}

func (c *OtelCore) Enabled(level zapcore.Level) bool {
	return level >= c.minLevel
}

func (c *OtelCore) With(fields []zapcore.Field) zapcore.Core {
	clone := *c
	clone.fields = append(append([]log.KeyValue{}, c.fields...), zapFieldsToKV(fields)...)
	return &clone
}

func (c *OtelCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return ce.AddCore(entry, c)
	}
	return ce
}

func (c *OtelCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	rec := log.Record{}
	rec.SetTimestamp(entry.Time)
	rec.SetBody(log.StringValue(entry.Message))
	rec.SetSeverity(zapSeverity(entry.Level))
	rec.SetSeverityText(entry.Level.String())
	rec.AddAttributes(append(c.fields, zapFieldsToKV(fields)...)...)
	c.logger.Emit(context.Background(), rec)
	return nil
}

func (c *OtelCore) Sync() error { return nil }

func zapSeverity(l zapcore.Level) log.Severity {
	switch l {
	case zapcore.DebugLevel:
		return log.SeverityDebug
	case zapcore.InfoLevel:
		return log.SeverityInfo
	case zapcore.WarnLevel:
		return log.SeverityWarn
	case zapcore.ErrorLevel:
		return log.SeverityError
	default:
		return log.SeverityFatal
	}
}

func zapFieldsToKV(fields []zapcore.Field) []log.KeyValue {
	kvs := make([]log.KeyValue, 0, len(fields))
	for _, f := range fields {
		kvs = append(kvs, zapFieldToKV(f))
	}
	return kvs
}

func zapFieldToKV(f zapcore.Field) log.KeyValue {
	switch f.Type {
	case zapcore.StringType:
		return log.String(f.Key, f.String)
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
		zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type,
		zapcore.UintptrType:
		return log.Int64(f.Key, f.Integer)
	case zapcore.Float64Type:
		return log.Float64(f.Key, math.Float64frombits(uint64(f.Integer)))
	case zapcore.Float32Type:
		return log.Float64(f.Key, float64(math.Float32frombits(uint32(f.Integer))))
	case zapcore.BoolType:
		return log.Bool(f.Key, f.Integer == 1)
	case zapcore.DurationType:
		return log.String(f.Key, time.Duration(f.Integer).String())
	case zapcore.ErrorType:
		if f.Interface != nil {
			return log.String(f.Key, f.Interface.(error).Error())
		}
		return log.String(f.Key, "")
	default:
		if f.Interface != nil {
			return log.String(f.Key, fmt.Sprintf("%v", f.Interface))
		}
		if f.String != "" {
			return log.String(f.Key, f.String)
		}
		return log.Int64(f.Key, f.Integer)
	}
}
