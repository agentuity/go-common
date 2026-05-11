package telemetry

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otelLog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

type durableLogTestExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
	batches []int
	err     error
}

func (e *durableLogTestExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.records = append(e.records, records...)
	e.batches = append(e.batches, len(records))
	return nil
}

func (e *durableLogTestExporter) Shutdown(context.Context) error {
	return nil
}

func (e *durableLogTestExporter) ForceFlush(context.Context) error {
	return nil
}

func (e *durableLogTestExporter) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *durableLogTestExporter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.records)
}

func (e *durableLogTestExporter) batchCounts() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.batches...)
}

func testLogRecord(msg string) sdklog.Record {
	r := sdklog.Record{}
	r.SetTimestamp(time.Now())
	r.SetObservedTimestamp(time.Now())
	r.SetSeverity(otelLog.SeverityInfo)
	r.SetSeverityText("Info")
	r.SetBody(otelLog.StringValue(msg))
	r.SetAttributes(otelLog.String("key", "value"))
	return r
}

func TestDurableLogProcessorForceFlushExportsRecords(t *testing.T) {
	exporter := &durableLogTestExporter{}
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path:           filepath.Join(t.TempDir(), "logs.db"),
		idleTimeout:    time.Hour,
		emitQueueSize:  10,
		writeBatchSize: 10,
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	for i := 0; i < 3; i++ {
		require.NoError(t, p.OnEmit(context.Background(), ptrRecord(testLogRecord("hello"))))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.ForceFlush(ctx))
	assert.Equal(t, 3, exporter.count())
}

func TestDurableLogProcessorKeepsRecordsOnExportFailure(t *testing.T) {
	exporter := &durableLogTestExporter{}
	exporter.setErr(errors.New("offline"))
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path:           filepath.Join(t.TempDir(), "logs.db"),
		idleTimeout:    time.Hour,
		emitQueueSize:  10,
		writeBatchSize: 1,
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	require.NoError(t, p.OnEmit(context.Background(), ptrRecord(testLogRecord("hello"))))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, p.ForceFlush(ctx))

	exporter.setErr(nil)
	require.NoError(t, p.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableLogProcessorNonBlockingDropsWhenEmitQueueFull(t *testing.T) {
	p := &durableLogProcessor{
		emitCh: make(chan queuedLogRecord, 1),
		wakeCh: make(chan struct{}, 1),
	}

	for i := 0; i < 1000; i++ {
		require.NoError(t, p.OnEmit(context.Background(), ptrRecord(testLogRecord("hello"))))
	}
	assert.Greater(t, p.dropped.Load(), uint64(0))
}

func TestDurableLogProcessorReplaysPersistedRecordsAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "logs.db")
	failing := &durableLogTestExporter{}
	failing.setErr(errors.New("offline"))
	p, err := newDurableLogProcessor(context.Background(), failing, durableLogConfig{
		path:           dbPath,
		idleTimeout:    time.Hour,
		emitQueueSize:  10,
		writeBatchSize: 1,
	})
	require.NoError(t, err)
	require.NoError(t, p.OnEmit(context.Background(), ptrRecord(testLogRecord("persisted"))))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, p.ForceFlush(ctx))
	assert.Error(t, p.Shutdown(ctx))

	exporter := &durableLogTestExporter{}
	restarted, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path:        dbPath,
		idleTimeout: time.Hour,
	})
	require.NoError(t, err)
	defer restarted.Shutdown(context.Background())
	require.NoError(t, restarted.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableLogProcessorStorageCapsDropOldest(t *testing.T) {
	exporter := &durableLogTestExporter{}
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path:             filepath.Join(t.TempDir(), "logs.db"),
		idleTimeout:      time.Hour,
		emitQueueSize:    10,
		writeBatchSize:   1,
		maxStoredRecords: 2,
		maxStoredBytes:   1 << 20,
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	for _, msg := range []string{"oldest", "middle", "newest"} {
		require.NoError(t, p.OnEmit(context.Background(), ptrRecord(testLogRecord(msg))))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.flushWriter(ctx))

	var count int
	require.NoError(t, p.db.QueryRow(`SELECT COUNT(*) FROM otel_log_queue`).Scan(&count))
	assert.Equal(t, 2, count)
	require.NoError(t, p.ForceFlush(ctx))
	assert.Equal(t, 2, exporter.count())
}

func TestDurableLogProcessorAutoVacuumIncremental(t *testing.T) {
	exporter := &durableLogTestExporter{}
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path: filepath.Join(t.TempDir(), "logs.db"),
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	var autoVacuum int
	require.NoError(t, p.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum))
	assert.Equal(t, 2, autoVacuum)
}

func TestDurableLogProcessorDropsCorruptRows(t *testing.T) {
	exporter := &durableLogTestExporter{}
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path: filepath.Join(t.TempDir(), "logs.db"),
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	_, err = p.db.Exec(`INSERT INTO otel_log_queue (created_at, size_bytes, record) VALUES (?, ?, ?)`, time.Now().UnixNano(), 3, []byte("bad"))
	require.NoError(t, err)
	require.NoError(t, p.insert([]queuedLogRecord{mustQueuedLogRecord(t, testLogRecord("valid"))}))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
	assertQueueEmpty(t, p.db, "otel_log_queue")
}

func TestDurableLogProcessorCoalescesReplayRows(t *testing.T) {
	exporter := &durableLogTestExporter{}
	p, err := newDurableLogProcessor(context.Background(), exporter, durableLogConfig{
		path:           filepath.Join(t.TempDir(), "logs.db"),
		idleTimeout:    time.Hour,
		emitQueueSize:  10,
		writeBatchSize: 10,
		maxRecords:     10,
		maxBytes:       1 << 20,
	})
	require.NoError(t, err)
	defer p.Shutdown(context.Background())

	records := make([]queuedLogRecord, 0, 3)
	for i := 0; i < 3; i++ {
		records = append(records, mustQueuedLogRecord(t, testLogRecord("batched")))
	}
	require.NoError(t, p.insert(records))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.ForceFlush(ctx))
	assert.Equal(t, 3, exporter.count())
	assert.Equal(t, []int{3}, exporter.batchCounts())
	assertQueueEmpty(t, p.db, "otel_log_queue")
}

func ptrRecord(r sdklog.Record) *sdklog.Record {
	return &r
}

func mustQueuedLogRecord(t *testing.T, r sdklog.Record) queuedLogRecord {
	t.Helper()
	qr, err := encodeQueuedRecord(r)
	require.NoError(t, err)
	return qr
}

func assertQueueEmpty(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM `+table).Scan(&count))
	assert.Equal(t, 0, count)
}
