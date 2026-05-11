package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	defaultMetricMaxStoredBatches = 10000
	defaultMetricMaxStoredBytes   = int64(256 * 1024 * 1024)
	defaultMetricReplayMaxRows    = 512
	defaultMetricReplayMaxBytes   = 512 * 1024
	defaultMetricRetryInitial     = time.Second
	defaultMetricRetryMax         = 30 * time.Second
)

type durableMetricConfig struct {
	path             string
	maxStoredBatches int
	maxStoredBytes   int64
	replayMaxRows    int
	replayMaxBytes   int
	retryInitial     time.Duration
	retryMax         time.Duration
}

func (c durableMetricConfig) withDefaults() durableMetricConfig {
	if c.path == "" {
		c.path = defaultMetricBatchPath()
	}
	if c.maxStoredBatches <= 0 {
		c.maxStoredBatches = defaultMetricMaxStoredBatches
	}
	if c.maxStoredBytes <= 0 {
		c.maxStoredBytes = defaultMetricMaxStoredBytes
	}
	if c.replayMaxRows <= 0 {
		c.replayMaxRows = defaultMetricReplayMaxRows
	}
	if c.replayMaxBytes <= 0 {
		c.replayMaxBytes = defaultMetricReplayMaxBytes
	}
	if c.retryInitial <= 0 {
		c.retryInitial = defaultMetricRetryInitial
	}
	if c.retryMax <= 0 {
		c.retryMax = defaultMetricRetryMax
	}
	return c
}

func defaultMetricBatchPath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "agentuity", "otel-metrics.db")
}

type durableMetricExporter struct {
	exporter sdkmetric.Exporter
	cfg      durableMetricConfig
	db       *sql.DB

	wakeCh chan struct{}
	done   chan struct{}
	wg     sync.WaitGroup

	stopped atomic.Bool
}

var _ sdkmetric.Exporter = (*durableMetricExporter)(nil)

func newDurableMetricExporter(ctx context.Context, exporter sdkmetric.Exporter, cfg durableMetricConfig) (*durableMetricExporter, error) {
	cfg = cfg.withDefaults()
	if err := os.MkdirAll(filepath.Dir(cfg.path), 0o755); err != nil && cfg.path != ":memory:" {
		return nil, err
	}
	db, err := sql.Open("sqlite", cfg.path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := configureDurableQueueDB(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS otel_metric_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_metric_queue ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_metric_queue ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `UPDATE otel_metric_queue SET size_bytes = length(payload) WHERE size_bytes = 0`)
	if err := ensureMetricQueueSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	e := &durableMetricExporter{
		exporter: exporter,
		cfg:      cfg,
		db:       db,
		wakeCh:   make(chan struct{}, 1),
		done:     make(chan struct{}),
	}
	e.wg.Add(1)
	go e.exportLoop()
	if e.hasRows() {
		e.wake()
	}
	return e, nil
}

func ensureMetricQueueSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info(otel_metric_queue)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	columns := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if columns["id"] && columns["created_at"] && columns["size_bytes"] && columns["payload"] {
		return nil
	}
	if _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS otel_metric_queue`); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `CREATE TABLE otel_metric_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		payload BLOB NOT NULL
	)`)
	return err
}

func (e *durableMetricExporter) Temporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	return e.exporter.Temporality(kind)
}

func (e *durableMetricExporter) Aggregation(kind sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return e.exporter.Aggregation(kind)
}

func (e *durableMetricExporter) Export(ctx context.Context, rm *metricdata.ResourceMetrics) error {
	if e.stopped.Load() {
		return nil
	}
	payload, err := encodeMetricPayload(rm)
	if err != nil {
		return err
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO otel_metric_queue (created_at, size_bytes, payload) VALUES (?, ?, ?)`,
		time.Now().UnixNano(), len(payload), payload); err != nil {
		return err
	}
	if err := e.enforceLimits(ctx); err != nil {
		return err
	}
	e.wake()
	return nil
}

func (e *durableMetricExporter) ForceFlush(ctx context.Context) error {
	return e.drain(ctx)
}

func (e *durableMetricExporter) Shutdown(ctx context.Context) error {
	if e.stopped.Swap(true) {
		return nil
	}
	close(e.done)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return errors.Join(ctx.Err(), e.exporter.Shutdown(ctx), e.db.Close())
	}
	err := e.drain(ctx)
	return errors.Join(err, e.exporter.Shutdown(ctx), e.db.Close())
}

func (e *durableMetricExporter) enforceLimits(ctx context.Context) error {
	_, err := e.db.ExecContext(ctx, `DELETE FROM otel_metric_queue
		WHERE id IN (
			SELECT id FROM otel_metric_queue
			ORDER BY id ASC
			LIMIT MAX((SELECT COUNT(*) FROM otel_metric_queue) - ?, 0)
		)`, e.cfg.maxStoredBatches)
	if err != nil {
		return err
	}
	var totalBytes int64
	if err := e.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM otel_metric_queue`).Scan(&totalBytes); err != nil {
		return err
	}
	for totalBytes > e.cfg.maxStoredBytes {
		var id int64
		var size int64
		if err := e.db.QueryRowContext(ctx, `SELECT id, size_bytes FROM otel_metric_queue ORDER BY id ASC LIMIT 1`).Scan(&id, &size); err != nil {
			return err
		}
		if _, err := e.db.ExecContext(ctx, `DELETE FROM otel_metric_queue WHERE id = ?`, id); err != nil {
			return err
		}
		totalBytes -= size
	}
	_, _ = e.db.ExecContext(ctx, `PRAGMA incremental_vacuum`)
	return err
}

func (e *durableMetricExporter) exportLoop() {
	defer e.wg.Done()
	backoff := e.cfg.retryInitial
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	for {
		select {
		case <-e.done:
			return
		case <-e.wakeCh:
			resetTimer(timer, time.Millisecond)
		case <-timer.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			err := e.exportBatch(ctx)
			cancel()
			if err != nil {
				otel.Handle(err)
				sleep := backoff + time.Duration(rand.Int63n(int64(backoff/2+1)))
				if backoff < e.cfg.retryMax {
					backoff *= 2
					if backoff > e.cfg.retryMax {
						backoff = e.cfg.retryMax
					}
				}
				resetTimer(timer, sleep)
				continue
			}
			backoff = e.cfg.retryInitial
			if e.hasRows() {
				resetTimer(timer, time.Millisecond)
			} else {
				resetTimer(timer, time.Hour)
			}
		}
	}
}

func (e *durableMetricExporter) drain(ctx context.Context) error {
	for {
		if !e.hasRows() {
			return e.exporter.ForceFlush(ctx)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.exportBatch(ctx); err != nil {
			return err
		}
	}
}

func (e *durableMetricExporter) exportBatch(ctx context.Context) error {
	rows, err := e.db.QueryContext(ctx, `SELECT id, payload, size_bytes FROM otel_metric_queue ORDER BY id ASC LIMIT ?`, e.cfg.replayMaxRows)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int64
	var resourceMetrics []metricdata.ResourceMetrics
	var totalBytes int
	for rows.Next() {
		var id int64
		var payload []byte
		var size int
		if err := rows.Scan(&id, &payload, &size); err != nil {
			return err
		}
		if len(ids) > 0 && totalBytes+size > e.cfg.replayMaxBytes {
			break
		}
		rm, err := decodeMetricPayload(payload)
		if err != nil {
			ids = append(ids, id)
			continue
		}
		ids = append(ids, id)
		resourceMetrics = append(resourceMetrics, rm)
		totalBytes += size
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(resourceMetrics) > 0 {
		merged := mergeResourceMetrics(resourceMetrics)
		if err := e.exporter.Export(ctx, &merged); err != nil {
			return err
		}
	}
	return e.deleteIDs(ctx, ids)
}

func (e *durableMetricExporter) deleteIDs(ctx context.Context, ids []int64) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM otel_metric_queue WHERE id = ?`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, id := range ids {
		if _, err := stmt.ExecContext(ctx, id); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
	}
	_ = stmt.Close()
	return tx.Commit()
}

func mergeResourceMetrics(items []metricdata.ResourceMetrics) metricdata.ResourceMetrics {
	if len(items) == 0 {
		return metricdata.ResourceMetrics{}
	}
	merged := metricdata.ResourceMetrics{
		Resource:     items[0].Resource,
		ScopeMetrics: make([]metricdata.ScopeMetrics, 0),
	}
	for _, item := range items {
		merged.ScopeMetrics = append(merged.ScopeMetrics, item.ScopeMetrics...)
	}
	return merged
}

func (e *durableMetricExporter) hasRows() bool {
	var n int
	return e.db.QueryRow(`SELECT 1 FROM otel_metric_queue LIMIT 1`).Scan(&n) == nil
}

func (e *durableMetricExporter) wake() {
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

type metricPayloadDTO struct {
	Resource     []attrDTO
	ResourceURL  string
	ScopeMetrics []scopeMetricsDTO
}

type scopeMetricsDTO struct {
	Scope   scopeDTO
	Metrics []metricDTO
}

type metricDTO struct {
	Name        string
	Description string
	Unit        string
	Data        aggregationDTO
}

type aggregationDTO struct {
	Kind        string
	Temporality int
	IsMonotonic bool
	IntPoints   []numberDataPointDTO[int64]
	FloatPoints []numberDataPointDTO[float64]
	IntHist     []histogramPointDTO[int64]
	FloatHist   []histogramPointDTO[float64]
	Summary     []summaryPointDTO
}

type numberDataPointDTO[N int64 | float64] struct {
	Attributes []attrDTO
	StartTime  time.Time
	Time       time.Time
	Value      N
}

type histogramPointDTO[N int64 | float64] struct {
	Attributes   []attrDTO
	StartTime    time.Time
	Time         time.Time
	Count        uint64
	Bounds       []float64
	BucketCounts []uint64
	Sum          N
}

type summaryPointDTO struct {
	Attributes     []attrDTO
	StartTime      time.Time
	Time           time.Time
	Count          uint64
	Sum            float64
	QuantileValues []metricdata.QuantileValue
}

func encodeMetricPayload(rm *metricdata.ResourceMetrics) ([]byte, error) {
	dto := metricPayloadDTO{}
	if rm.Resource != nil {
		dto.ResourceURL = rm.Resource.SchemaURL()
		iter := rm.Resource.Iter()
		for iter.Next() {
			dto.Resource = append(dto.Resource, attrToDTO(iter.Attribute()))
		}
	}
	for _, sm := range rm.ScopeMetrics {
		smd := scopeMetricsDTO{
			Scope: scopeDTO{
				Name:      sm.Scope.Name,
				Version:   sm.Scope.Version,
				SchemaURL: sm.Scope.SchemaURL,
			},
		}
		iter := sm.Scope.Attributes.Iter()
		for iter.Next() {
			smd.Scope.Attributes = append(smd.Scope.Attributes, attrToDTO(iter.Attribute()))
		}
		for _, m := range sm.Metrics {
			md := metricDTO{Name: m.Name, Description: m.Description, Unit: m.Unit}
			switch data := m.Data.(type) {
			case metricdata.Gauge[int64]:
				md.Data.Kind = "gauge_int"
				md.Data.IntPoints = numberPointsToDTO(data.DataPoints)
			case metricdata.Gauge[float64]:
				md.Data.Kind = "gauge_float"
				md.Data.FloatPoints = numberPointsToDTO(data.DataPoints)
			case metricdata.Sum[int64]:
				md.Data.Kind = "sum_int"
				md.Data.Temporality = int(data.Temporality)
				md.Data.IsMonotonic = data.IsMonotonic
				md.Data.IntPoints = numberPointsToDTO(data.DataPoints)
			case metricdata.Sum[float64]:
				md.Data.Kind = "sum_float"
				md.Data.Temporality = int(data.Temporality)
				md.Data.IsMonotonic = data.IsMonotonic
				md.Data.FloatPoints = numberPointsToDTO(data.DataPoints)
			case metricdata.Histogram[int64]:
				md.Data.Kind = "hist_int"
				md.Data.Temporality = int(data.Temporality)
				md.Data.IntHist = histogramPointsToDTO(data.DataPoints)
			case metricdata.Histogram[float64]:
				md.Data.Kind = "hist_float"
				md.Data.Temporality = int(data.Temporality)
				md.Data.FloatHist = histogramPointsToDTO(data.DataPoints)
			case metricdata.Summary:
				md.Data.Kind = "summary"
				md.Data.Summary = summaryPointsToDTO(data.DataPoints)
			default:
				continue
			}
			smd.Metrics = append(smd.Metrics, md)
		}
		dto.ScopeMetrics = append(dto.ScopeMetrics, smd)
	}
	return json.Marshal(dto)
}

func decodeMetricPayload(payload []byte) (metricdata.ResourceMetrics, error) {
	var dto metricPayloadDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		return metricdata.ResourceMetrics{}, err
	}
	rm := metricdata.ResourceMetrics{
		Resource: resourceFromDTO(dto.ResourceURL, dto.Resource),
	}
	for _, smd := range dto.ScopeMetrics {
		sm := metricdata.ScopeMetrics{
			Scope: scopeFromDTO(smd.Scope),
		}
		for _, md := range smd.Metrics {
			m := metricdata.Metrics{Name: md.Name, Description: md.Description, Unit: md.Unit}
			switch md.Data.Kind {
			case "gauge_int":
				m.Data = metricdata.Gauge[int64]{DataPoints: numberPointsFromDTO(md.Data.IntPoints)}
			case "gauge_float":
				m.Data = metricdata.Gauge[float64]{DataPoints: numberPointsFromDTO(md.Data.FloatPoints)}
			case "sum_int":
				m.Data = metricdata.Sum[int64]{DataPoints: numberPointsFromDTO(md.Data.IntPoints), Temporality: metricdata.Temporality(md.Data.Temporality), IsMonotonic: md.Data.IsMonotonic}
			case "sum_float":
				m.Data = metricdata.Sum[float64]{DataPoints: numberPointsFromDTO(md.Data.FloatPoints), Temporality: metricdata.Temporality(md.Data.Temporality), IsMonotonic: md.Data.IsMonotonic}
			case "hist_int":
				m.Data = metricdata.Histogram[int64]{DataPoints: histogramPointsFromDTO(md.Data.IntHist), Temporality: metricdata.Temporality(md.Data.Temporality)}
			case "hist_float":
				m.Data = metricdata.Histogram[float64]{DataPoints: histogramPointsFromDTO(md.Data.FloatHist), Temporality: metricdata.Temporality(md.Data.Temporality)}
			case "summary":
				m.Data = metricdata.Summary{DataPoints: summaryPointsFromDTO(md.Data.Summary)}
			default:
				continue
			}
			sm.Metrics = append(sm.Metrics, m)
		}
		rm.ScopeMetrics = append(rm.ScopeMetrics, sm)
	}
	return rm, nil
}

func numberPointsToDTO[N int64 | float64](points []metricdata.DataPoint[N]) []numberDataPointDTO[N] {
	out := make([]numberDataPointDTO[N], 0, len(points))
	for _, p := range points {
		out = append(out, numberDataPointDTO[N]{
			Attributes: attrsSetToDTO(p.Attributes),
			StartTime:  p.StartTime,
			Time:       p.Time,
			Value:      p.Value,
		})
	}
	return out
}

func numberPointsFromDTO[N int64 | float64](points []numberDataPointDTO[N]) []metricdata.DataPoint[N] {
	out := make([]metricdata.DataPoint[N], 0, len(points))
	for _, p := range points {
		out = append(out, metricdata.DataPoint[N]{
			Attributes: attribute.NewSet(attrsFromDTO(p.Attributes)...),
			StartTime:  p.StartTime,
			Time:       p.Time,
			Value:      p.Value,
		})
	}
	return out
}

func histogramPointsToDTO[N int64 | float64](points []metricdata.HistogramDataPoint[N]) []histogramPointDTO[N] {
	out := make([]histogramPointDTO[N], 0, len(points))
	for _, p := range points {
		out = append(out, histogramPointDTO[N]{
			Attributes:   attrsSetToDTO(p.Attributes),
			StartTime:    p.StartTime,
			Time:         p.Time,
			Count:        p.Count,
			Bounds:       append([]float64(nil), p.Bounds...),
			BucketCounts: append([]uint64(nil), p.BucketCounts...),
			Sum:          p.Sum,
		})
	}
	return out
}

func histogramPointsFromDTO[N int64 | float64](points []histogramPointDTO[N]) []metricdata.HistogramDataPoint[N] {
	out := make([]metricdata.HistogramDataPoint[N], 0, len(points))
	for _, p := range points {
		out = append(out, metricdata.HistogramDataPoint[N]{
			Attributes:   attribute.NewSet(attrsFromDTO(p.Attributes)...),
			StartTime:    p.StartTime,
			Time:         p.Time,
			Count:        p.Count,
			Bounds:       append([]float64(nil), p.Bounds...),
			BucketCounts: append([]uint64(nil), p.BucketCounts...),
			Sum:          p.Sum,
		})
	}
	return out
}

func summaryPointsToDTO(points []metricdata.SummaryDataPoint) []summaryPointDTO {
	out := make([]summaryPointDTO, 0, len(points))
	for _, p := range points {
		out = append(out, summaryPointDTO{
			Attributes:     attrsSetToDTO(p.Attributes),
			StartTime:      p.StartTime,
			Time:           p.Time,
			Count:          p.Count,
			Sum:            p.Sum,
			QuantileValues: append([]metricdata.QuantileValue(nil), p.QuantileValues...),
		})
	}
	return out
}

func summaryPointsFromDTO(points []summaryPointDTO) []metricdata.SummaryDataPoint {
	out := make([]metricdata.SummaryDataPoint, 0, len(points))
	for _, p := range points {
		out = append(out, metricdata.SummaryDataPoint{
			Attributes:     attribute.NewSet(attrsFromDTO(p.Attributes)...),
			StartTime:      p.StartTime,
			Time:           p.Time,
			Count:          p.Count,
			Sum:            p.Sum,
			QuantileValues: append([]metricdata.QuantileValue(nil), p.QuantileValues...),
		})
	}
	return out
}

func attrsSetToDTO(set attribute.Set) []attrDTO {
	iter := set.Iter()
	out := make([]attrDTO, 0, set.Len())
	for iter.Next() {
		out = append(out, attrToDTO(iter.Attribute()))
	}
	return out
}

func resourceFromDTO(schemaURL string, attrs []attrDTO) *resource.Resource {
	return resource.NewWithAttributes(schemaURL, attrsFromDTO(attrs)...)
}

func scopeFromDTO(dto scopeDTO) instrumentation.Scope {
	return instrumentation.Scope{
		Name:       dto.Name,
		Version:    dto.Version,
		SchemaURL:  dto.SchemaURL,
		Attributes: attribute.NewSet(attrsFromDTO(dto.Attributes)...),
	}
}
