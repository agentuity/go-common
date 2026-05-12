package telemetry

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

type durableTraceTestExporter struct {
	mu      sync.Mutex
	spans   []sdktrace.ReadOnlySpan
	batches []int
	err     error
}

func (e *durableTraceTestExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.spans = append(e.spans, spans...)
	e.batches = append(e.batches, len(spans))
	return nil
}

func (e *durableTraceTestExporter) Shutdown(context.Context) error {
	return nil
}

func (e *durableTraceTestExporter) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *durableTraceTestExporter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.spans)
}

func (e *durableTraceTestExporter) batchCounts() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.batches...)
}

func testReadOnlySpan() sdktrace.ReadOnlySpan {
	tid := trace.TraceID([16]byte{1})
	sid := trace.SpanID([8]byte{2})
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.FlagsSampled,
	})
	now := time.Now()
	return tracetest.SpanStub{
		Name:        "test-span",
		SpanContext: sc,
		SpanKind:    trace.SpanKindServer,
		StartTime:   now.Add(-time.Millisecond),
		EndTime:     now,
		Attributes:  []attribute.KeyValue{attribute.String("route", "/")},
	}.Snapshot()
}

func TestDurableTraceExporterShutdownExportsBatches(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)

	require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.Shutdown(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableTraceExporterKeepsBatchesOnExportFailure(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	exporter.setErr(errors.New("offline"))
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)

	require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	stopDurableTraceExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, e.drain(ctx))

	exporter.setErr(nil)
	require.NoError(t, e.drain(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableTraceExporterReplaysPersistedBatchesAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "traces.db")
	failing := &durableTraceTestExporter{}
	failing.setErr(errors.New("offline"))
	e, err := newDurableTraceExporter(context.Background(), failing, durableTraceConfig{
		path: dbPath,
	})
	require.NoError(t, err)
	require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	stopDurableTraceExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, e.drain(ctx))

	exporter := &durableTraceTestExporter{}
	restarted, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: dbPath,
	})
	require.NoError(t, err)
	defer restarted.Shutdown(context.Background())
	require.Eventually(t, func() bool {
		return exporter.count() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestDurableTraceExporterStorageCapsDropOldest(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path:             filepath.Join(t.TempDir(), "traces.db"),
		maxStoredBatches: 2,
		maxStoredBytes:   1 << 20,
	})
	require.NoError(t, err)
	stopDurableTraceExporterLoop(e)
	defer e.db.Close()

	for i := 0; i < 3; i++ {
		require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	}
	var count int
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM otel_trace_queue`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestDurableTraceExporterAutoVacuumIncremental(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)
	defer e.Shutdown(context.Background())

	var autoVacuum int
	require.NoError(t, e.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum))
	assert.Equal(t, 2, autoVacuum)
}

func TestDurableTraceExporterDropsCorruptRows(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)

	_, err = e.db.Exec(`INSERT INTO otel_trace_queue (created_at, size_bytes, payload) VALUES (?, ?, ?)`, time.Now().UnixNano(), 3, []byte("bad"))
	require.NoError(t, err)
	require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	stopDurableTraceExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.drain(ctx))
	assert.Equal(t, 1, exporter.count())
	assertQueueEmpty(t, e.db, "otel_trace_queue")
}

func TestDurableTraceExporterCoalescesReplayRows(t *testing.T) {
	exporter := &durableTraceTestExporter{}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path:           filepath.Join(t.TempDir(), "traces.db"),
		replayMaxRows:  10,
		replayMaxBytes: 1 << 20,
	})
	require.NoError(t, err)
	stopDurableTraceExporterLoop(e)
	defer e.db.Close()

	for i := 0; i < 3; i++ {
		require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.drain(ctx))
	assert.Equal(t, 3, exporter.count())
	assert.Equal(t, []int{3}, exporter.batchCounts())
	assertQueueEmpty(t, e.db, "otel_trace_queue")
}

func TestDurableTraceExporterDoesNotHoldDBConnectionDuringReplayExport(t *testing.T) {
	exporter := &blockingSpanExporter{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	e, err := newDurableTraceExporter(context.Background(), exporter, durableTraceConfig{
		path: filepath.Join(t.TempDir(), "traces.db"),
	})
	require.NoError(t, err)

	require.NoError(t, e.ExportSpans(context.Background(), []sdktrace.ReadOnlySpan{testReadOnlySpan()}))
	require.Eventually(t, func() bool {
		select {
		case <-exporter.started:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.NoError(t, e.ExportSpans(ctx, []sdktrace.ReadOnlySpan{testReadOnlySpan()}))

	close(exporter.release)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	require.NoError(t, e.Shutdown(shutdownCtx))
}

func stopDurableTraceExporterLoop(e *durableTraceExporter) {
	if e.loopCancel != nil {
		e.loopCancel()
	}
	close(e.done)
	e.wg.Wait()
}
