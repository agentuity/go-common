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
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type durableMetricTestExporter struct {
	mu      sync.Mutex
	metrics []metricdata.ResourceMetrics
	batches []int
	err     error
}

func (e *durableMetricTestExporter) Temporality(sdkmetric.InstrumentKind) metricdata.Temporality {
	return metricdata.CumulativeTemporality
}

func (e *durableMetricTestExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(kind)
}

func (e *durableMetricTestExporter) Export(_ context.Context, rm *metricdata.ResourceMetrics) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.metrics = append(e.metrics, *rm)
	e.batches = append(e.batches, countMetricDataPoints(rm))
	return nil
}

func (e *durableMetricTestExporter) ForceFlush(context.Context) error {
	return nil
}

func (e *durableMetricTestExporter) Shutdown(context.Context) error {
	return nil
}

func (e *durableMetricTestExporter) setErr(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.err = err
}

func (e *durableMetricTestExporter) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.metrics)
}

func (e *durableMetricTestExporter) batchCounts() []int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]int(nil), e.batches...)
}

func testResourceMetrics() metricdata.ResourceMetrics {
	now := time.Now()
	return metricdata.ResourceMetrics{
		ScopeMetrics: []metricdata.ScopeMetrics{
			{
				Metrics: []metricdata.Metrics{
					{
						Name: "requests",
						Unit: "1",
						Data: metricdata.Sum[int64]{
							Temporality: metricdata.CumulativeTemporality,
							IsMonotonic: true,
							DataPoints: []metricdata.DataPoint[int64]{
								{
									Attributes: attribute.NewSet(attribute.String("route", "/")),
									StartTime:  now.Add(-time.Minute),
									Time:       now,
									Value:      3,
								},
							},
						},
					},
				},
			},
		},
	}
}

func TestDurableMetricExporterForceFlushExportsBatches(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path: filepath.Join(t.TempDir(), "metrics.db"),
	})
	require.NoError(t, err)

	rm := testResourceMetrics()
	require.NoError(t, e.Export(context.Background(), &rm))
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableMetricExporterKeepsBatchesOnExportFailure(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	exporter.setErr(errors.New("offline"))
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path: filepath.Join(t.TempDir(), "metrics.db"),
	})
	require.NoError(t, err)

	rm := testResourceMetrics()
	require.NoError(t, e.Export(context.Background(), &rm))
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, e.ForceFlush(ctx))

	exporter.setErr(nil)
	require.NoError(t, e.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableMetricExporterReplaysPersistedBatchesAfterRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	failing := &durableMetricTestExporter{}
	failing.setErr(errors.New("offline"))
	e, err := newDurableMetricExporter(context.Background(), failing, durableMetricConfig{
		path: dbPath,
	})
	require.NoError(t, err)
	rm := testResourceMetrics()
	require.NoError(t, e.Export(context.Background(), &rm))
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	assert.Error(t, e.ForceFlush(ctx))

	exporter := &durableMetricTestExporter{}
	restarted, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path: dbPath,
	})
	require.NoError(t, err)
	defer restarted.Shutdown(context.Background())
	require.NoError(t, restarted.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
}

func TestDurableMetricExporterStorageCapsDropOldest(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path:             filepath.Join(t.TempDir(), "metrics.db"),
		maxStoredBatches: 2,
		maxStoredBytes:   1 << 20,
	})
	require.NoError(t, err)
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	for i := 0; i < 3; i++ {
		rm := testResourceMetrics()
		require.NoError(t, e.Export(context.Background(), &rm))
	}
	var count int
	require.NoError(t, e.db.QueryRow(`SELECT COUNT(*) FROM otel_metric_queue`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestDurableMetricExporterAutoVacuumIncremental(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path: filepath.Join(t.TempDir(), "metrics.db"),
	})
	require.NoError(t, err)
	defer e.Shutdown(context.Background())

	var autoVacuum int
	require.NoError(t, e.db.QueryRow(`PRAGMA auto_vacuum`).Scan(&autoVacuum))
	assert.Equal(t, 2, autoVacuum)
}

func TestDurableMetricExporterDropsCorruptRows(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path: filepath.Join(t.TempDir(), "metrics.db"),
	})
	require.NoError(t, err)

	_, err = e.db.Exec(`INSERT INTO otel_metric_queue (created_at, size_bytes, payload) VALUES (?, ?, ?)`, time.Now().UnixNano(), 3, []byte("bad"))
	require.NoError(t, err)
	rm := testResourceMetrics()
	require.NoError(t, e.Export(context.Background(), &rm))
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
	assertQueueEmpty(t, e.db, "otel_metric_queue")
}

func TestDurableMetricExporterCoalescesReplayRows(t *testing.T) {
	exporter := &durableMetricTestExporter{}
	e, err := newDurableMetricExporter(context.Background(), exporter, durableMetricConfig{
		path:           filepath.Join(t.TempDir(), "metrics.db"),
		replayMaxRows:  10,
		replayMaxBytes: 1 << 20,
	})
	require.NoError(t, err)
	stopDurableMetricExporterLoop(e)
	defer e.db.Close()

	for i := 0; i < 3; i++ {
		rm := testResourceMetrics()
		require.NoError(t, e.Export(context.Background(), &rm))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, e.ForceFlush(ctx))
	assert.Equal(t, 1, exporter.count())
	assert.Equal(t, []int{3}, exporter.batchCounts())
	assertQueueEmpty(t, e.db, "otel_metric_queue")
}

func stopDurableMetricExporterLoop(t *durableMetricExporter) {
	close(t.done)
	t.wg.Wait()
}

func countMetricDataPoints(rm *metricdata.ResourceMetrics) int {
	var count int
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				count += len(data.DataPoints)
			case metricdata.Gauge[float64]:
				count += len(data.DataPoints)
			case metricdata.Sum[int64]:
				count += len(data.DataPoints)
			case metricdata.Sum[float64]:
				count += len(data.DataPoints)
			case metricdata.Histogram[int64]:
				count += len(data.DataPoints)
			case metricdata.Histogram[float64]:
				count += len(data.DataPoints)
			case metricdata.Summary:
				count += len(data.DataPoints)
			}
		}
	}
	return count
}
