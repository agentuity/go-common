package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultTraceMaxStoredBatches = 10000
	defaultTraceMaxStoredBytes   = int64(256 * 1024 * 1024)
	defaultTraceReplayMaxRows    = 512
	defaultTraceReplayMaxBytes   = 512 * 1024
	defaultTraceRetryInitial     = time.Second
	defaultTraceRetryMax         = 30 * time.Second
)

type durableTraceConfig struct {
	path             string
	maxStoredBatches int
	maxStoredBytes   int64
	replayMaxRows    int
	replayMaxBytes   int
	retryInitial     time.Duration
	retryMax         time.Duration
}

func (c durableTraceConfig) withDefaults() durableTraceConfig {
	if c.path == "" {
		c.path = defaultTraceBatchPath()
	}
	if c.maxStoredBatches <= 0 {
		c.maxStoredBatches = defaultTraceMaxStoredBatches
	}
	if c.maxStoredBytes <= 0 {
		c.maxStoredBytes = defaultTraceMaxStoredBytes
	}
	if c.replayMaxRows <= 0 {
		c.replayMaxRows = defaultTraceReplayMaxRows
	}
	if c.replayMaxBytes <= 0 {
		c.replayMaxBytes = defaultTraceReplayMaxBytes
	}
	if c.retryInitial <= 0 {
		c.retryInitial = defaultTraceRetryInitial
	}
	if c.retryMax <= 0 {
		c.retryMax = defaultTraceRetryMax
	}
	return c
}

func defaultTraceBatchPath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "agentuity", "otel-traces.db")
}

type durableTraceExporter struct {
	exporter sdktrace.SpanExporter
	cfg      durableTraceConfig
	db       *sql.DB

	wakeCh     chan struct{}
	done       chan struct{}
	wg         sync.WaitGroup
	loopCancel context.CancelFunc

	stopped atomic.Bool
}

var _ sdktrace.SpanExporter = (*durableTraceExporter)(nil)

func newDurableTraceExporter(ctx context.Context, exporter sdktrace.SpanExporter, cfg durableTraceConfig) (*durableTraceExporter, error) {
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS otel_trace_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		payload BLOB NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_trace_queue ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_trace_queue ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `UPDATE otel_trace_queue SET size_bytes = length(payload) WHERE size_bytes = 0`)

	loopCtx, loopCancel := context.WithCancel(context.Background())
	e := &durableTraceExporter{
		exporter:   exporter,
		cfg:        cfg,
		db:         db,
		wakeCh:     make(chan struct{}, 1),
		done:       make(chan struct{}),
		loopCancel: loopCancel,
	}
	e.wg.Add(1)
	go e.exportLoop(loopCtx)
	if e.hasRows() {
		e.wake()
	}
	return e, nil
}

func (e *durableTraceExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.stopped.Load() || len(spans) == 0 {
		return nil
	}
	payload, err := encodeTracePayload(spans)
	if err != nil {
		return err
	}
	if _, err := e.db.ExecContext(ctx, `INSERT INTO otel_trace_queue (created_at, size_bytes, payload) VALUES (?, ?, ?)`,
		time.Now().UnixNano(), len(payload), payload); err != nil {
		return err
	}
	if err := e.enforceLimits(ctx); err != nil {
		return err
	}
	e.wake()
	return nil
}

func (e *durableTraceExporter) Shutdown(ctx context.Context) error {
	if e.stopped.Swap(true) {
		return nil
	}
	if e.loopCancel != nil {
		e.loopCancel()
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

func (e *durableTraceExporter) enforceLimits(ctx context.Context) error {
	_, err := e.db.ExecContext(ctx, `DELETE FROM otel_trace_queue
		WHERE id IN (
			SELECT id FROM otel_trace_queue
			ORDER BY id ASC
			LIMIT MAX((SELECT COUNT(*) FROM otel_trace_queue) - ?, 0)
		)`, e.cfg.maxStoredBatches)
	if err != nil {
		return err
	}
	var totalBytes int64
	if err := e.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size_bytes), 0) FROM otel_trace_queue`).Scan(&totalBytes); err != nil {
		return err
	}
	for totalBytes > e.cfg.maxStoredBytes {
		var id int64
		var size int64
		if err := e.db.QueryRowContext(ctx, `SELECT id, size_bytes FROM otel_trace_queue ORDER BY id ASC LIMIT 1`).Scan(&id, &size); err != nil {
			return err
		}
		if _, err := e.db.ExecContext(ctx, `DELETE FROM otel_trace_queue WHERE id = ?`, id); err != nil {
			return err
		}
		totalBytes -= size
	}
	_, _ = e.db.ExecContext(ctx, `PRAGMA incremental_vacuum`)
	return nil
}

func (e *durableTraceExporter) exportLoop(loopCtx context.Context) {
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
			ctx, cancel := context.WithTimeout(loopCtx, 30*time.Second)
			err := e.exportBatch(ctx)
			cancel()
			if err != nil {
				otel.Handle(err)
				if backoff < e.cfg.retryMax {
					backoff *= 2
					if backoff > e.cfg.retryMax {
						backoff = e.cfg.retryMax
					}
				}
				resetTimer(timer, backoff)
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

func (e *durableTraceExporter) drain(ctx context.Context) error {
	for {
		if !e.hasRows() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.exportBatch(ctx); err != nil {
			return err
		}
	}
}

func (e *durableTraceExporter) exportBatch(ctx context.Context) error {
	rows, err := e.db.QueryContext(ctx, `SELECT id, payload, size_bytes FROM otel_trace_queue ORDER BY id ASC LIMIT ?`, e.cfg.replayMaxRows)
	if err != nil {
		return err
	}

	var ids []int64
	var spans []sdktrace.ReadOnlySpan
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
		decoded, err := decodeTracePayload(payload)
		if err != nil {
			ids = append(ids, id)
			continue
		}
		ids = append(ids, id)
		spans = append(spans, decoded...)
		totalBytes += size
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(spans) > 0 {
		if err := e.exporter.ExportSpans(ctx, spans); err != nil {
			return err
		}
	}
	return e.deleteIDs(ctx, ids)
}

func (e *durableTraceExporter) deleteIDs(ctx context.Context, ids []int64) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM otel_trace_queue WHERE id = ?`)
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

func (e *durableTraceExporter) hasRows() bool {
	var n int
	return e.db.QueryRow(`SELECT 1 FROM otel_trace_queue LIMIT 1`).Scan(&n) == nil
}

func (e *durableTraceExporter) wake() {
	select {
	case e.wakeCh <- struct{}{}:
	default:
	}
}

type tracePayloadDTO struct {
	Spans []spanDTO
}

type spanDTO struct {
	Name                 string
	SpanContext          spanContextDTO
	Parent               spanContextDTO
	SpanKind             int
	StartTime            time.Time
	EndTime              time.Time
	Attributes           []attrDTO
	Events               []spanEventDTO
	Links                []spanLinkDTO
	StatusCode           int
	StatusDescription    string
	DroppedAttributes    int
	DroppedEvents        int
	DroppedLinks         int
	ChildSpanCount       int
	Resource             []attrDTO
	ResourceURL          string
	InstrumentationScope scopeDTO
}

type spanContextDTO struct {
	TraceID    []byte
	SpanID     []byte
	TraceFlags byte
	TraceState string
	Remote     bool
}

type spanEventDTO struct {
	Name                  string
	Attributes            []attrDTO
	DroppedAttributeCount int
	Time                  time.Time
}

type spanLinkDTO struct {
	SpanContext           spanContextDTO
	Attributes            []attrDTO
	DroppedAttributeCount int
}

func encodeTracePayload(spans []sdktrace.ReadOnlySpan) ([]byte, error) {
	dto := tracePayloadDTO{Spans: make([]spanDTO, 0, len(spans))}
	for _, s := range spans {
		sd := spanDTO{
			Name:              s.Name(),
			SpanContext:       spanContextToDTO(s.SpanContext()),
			Parent:            spanContextToDTO(s.Parent()),
			SpanKind:          int(s.SpanKind()),
			StartTime:         s.StartTime(),
			EndTime:           s.EndTime(),
			DroppedAttributes: s.DroppedAttributes(),
			DroppedEvents:     s.DroppedEvents(),
			DroppedLinks:      s.DroppedLinks(),
			ChildSpanCount:    s.ChildSpanCount(),
			StatusCode:        int(s.Status().Code),
			StatusDescription: s.Status().Description,
		}
		for _, kv := range s.Attributes() {
			sd.Attributes = append(sd.Attributes, attrToDTO(kv))
		}
		for _, ev := range s.Events() {
			sd.Events = append(sd.Events, spanEventDTO{
				Name:                  ev.Name,
				Attributes:            attrsToDTO(ev.Attributes),
				DroppedAttributeCount: ev.DroppedAttributeCount,
				Time:                  ev.Time,
			})
		}
		for _, link := range s.Links() {
			sd.Links = append(sd.Links, spanLinkDTO{
				SpanContext:           spanContextToDTO(link.SpanContext),
				Attributes:            attrsToDTO(link.Attributes),
				DroppedAttributeCount: link.DroppedAttributeCount,
			})
		}
		if res := s.Resource(); res != nil {
			sd.ResourceURL = res.SchemaURL()
			iter := res.Iter()
			for iter.Next() {
				sd.Resource = append(sd.Resource, attrToDTO(iter.Attribute()))
			}
		}
		scope := s.InstrumentationScope()
		sd.InstrumentationScope = scopeDTO{Name: scope.Name, Version: scope.Version, SchemaURL: scope.SchemaURL}
		iter := scope.Attributes.Iter()
		for iter.Next() {
			sd.InstrumentationScope.Attributes = append(sd.InstrumentationScope.Attributes, attrToDTO(iter.Attribute()))
		}
		dto.Spans = append(dto.Spans, sd)
	}
	return json.Marshal(dto)
}

func decodeTracePayload(payload []byte) ([]sdktrace.ReadOnlySpan, error) {
	var dto tracePayloadDTO
	if err := json.Unmarshal(payload, &dto); err != nil {
		return nil, err
	}
	stubs := make(tracetest.SpanStubs, 0, len(dto.Spans))
	for _, sd := range dto.Spans {
		events := make([]sdktrace.Event, 0, len(sd.Events))
		for _, ev := range sd.Events {
			events = append(events, sdktrace.Event{
				Name:                  ev.Name,
				Attributes:            attrsFromDTO(ev.Attributes),
				DroppedAttributeCount: ev.DroppedAttributeCount,
				Time:                  ev.Time,
			})
		}
		links := make([]sdktrace.Link, 0, len(sd.Links))
		for _, link := range sd.Links {
			links = append(links, sdktrace.Link{
				SpanContext:           spanContextFromDTO(link.SpanContext),
				Attributes:            attrsFromDTO(link.Attributes),
				DroppedAttributeCount: link.DroppedAttributeCount,
			})
		}
		stubs = append(stubs, tracetest.SpanStub{
			Name:                 sd.Name,
			SpanContext:          spanContextFromDTO(sd.SpanContext),
			Parent:               spanContextFromDTO(sd.Parent),
			SpanKind:             trace.SpanKind(sd.SpanKind),
			StartTime:            sd.StartTime,
			EndTime:              sd.EndTime,
			Attributes:           attrsFromDTO(sd.Attributes),
			Events:               events,
			Links:                links,
			Status:               sdktrace.Status{Code: codes.Code(sd.StatusCode), Description: sd.StatusDescription},
			DroppedAttributes:    sd.DroppedAttributes,
			DroppedEvents:        sd.DroppedEvents,
			DroppedLinks:         sd.DroppedLinks,
			ChildSpanCount:       sd.ChildSpanCount,
			Resource:             resourceFromDTO(sd.ResourceURL, sd.Resource),
			InstrumentationScope: scopeFromDTO(sd.InstrumentationScope),
		})
	}
	return stubs.Snapshots(), nil
}

func attrsToDTO(attrs []attribute.KeyValue) []attrDTO {
	out := make([]attrDTO, 0, len(attrs))
	for _, kv := range attrs {
		out = append(out, attrToDTO(kv))
	}
	return out
}

func spanContextToDTO(sc trace.SpanContext) spanContextDTO {
	dto := spanContextDTO{
		TraceFlags: byte(sc.TraceFlags()),
		TraceState: sc.TraceState().String(),
		Remote:     sc.IsRemote(),
	}
	if tid := sc.TraceID(); tid.IsValid() {
		dto.TraceID = tid[:]
	}
	if sid := sc.SpanID(); sid.IsValid() {
		dto.SpanID = sid[:]
	}
	return dto
}

func spanContextFromDTO(dto spanContextDTO) trace.SpanContext {
	var tid trace.TraceID
	if len(dto.TraceID) == 16 {
		copy(tid[:], dto.TraceID)
	}
	var sid trace.SpanID
	if len(dto.SpanID) == 8 {
		copy(sid[:], dto.SpanID)
	}
	ts, _ := trace.ParseTraceState(dto.TraceState)
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    tid,
		SpanID:     sid,
		TraceFlags: trace.TraceFlags(dto.TraceFlags),
		TraceState: ts,
		Remote:     dto.Remote,
	})
}
