package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelLog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/sdk/instrumentation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"
	_ "modernc.org/sqlite"
)

const (
	defaultLogEmitQueueSize     = 4096
	defaultLogWriteBatchSize    = 128
	defaultLogWriteInterval     = 250 * time.Millisecond
	defaultLogBatchMaxRecords   = 512
	defaultLogBatchMaxBytes     = 512 * 1024
	defaultLogBatchIdleTimeout  = time.Second
	defaultLogMaxStoredRecords  = 100000
	defaultLogMaxStoredBytes    = int64(256 * 1024 * 1024)
	defaultLogRetryInitial      = time.Second
	defaultLogRetryMax          = 30 * time.Second
	defaultLogSQLiteBusyTimeout = 5000
)

type durableLogConfig struct {
	path             string
	emitQueueSize    int
	writeBatchSize   int
	writeInterval    time.Duration
	maxRecords       int
	maxBytes         int
	idleTimeout      time.Duration
	maxStoredRecords int
	maxStoredBytes   int64
	retryInitial     time.Duration
	retryMax         time.Duration
}

func (c durableLogConfig) withDefaults() durableLogConfig {
	if c.path == "" {
		c.path = defaultLogBatchPath()
	}
	if c.emitQueueSize <= 0 {
		c.emitQueueSize = defaultLogEmitQueueSize
	}
	if c.writeBatchSize <= 0 {
		c.writeBatchSize = defaultLogWriteBatchSize
	}
	if c.writeInterval <= 0 {
		c.writeInterval = defaultLogWriteInterval
	}
	if c.maxRecords <= 0 {
		c.maxRecords = defaultLogBatchMaxRecords
	}
	if c.maxBytes <= 0 {
		c.maxBytes = defaultLogBatchMaxBytes
	}
	if c.idleTimeout <= 0 {
		c.idleTimeout = defaultLogBatchIdleTimeout
	}
	if c.maxStoredRecords <= 0 {
		c.maxStoredRecords = defaultLogMaxStoredRecords
	}
	if c.maxStoredBytes <= 0 {
		c.maxStoredBytes = defaultLogMaxStoredBytes
	}
	if c.retryInitial <= 0 {
		c.retryInitial = defaultLogRetryInitial
	}
	if c.retryMax <= 0 {
		c.retryMax = defaultLogRetryMax
	}
	return c
}

func defaultLogBatchPath() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "agentuity", "otel-logs.db")
}

type durableLogProcessor struct {
	exporter sdklog.Exporter
	cfg      durableLogConfig
	db       *sql.DB

	emitCh   chan queuedLogRecord
	wakeCh   chan struct{}
	flushCh  chan chan struct{}
	done     chan struct{}
	wg       sync.WaitGroup
	replayMu sync.Mutex

	stopped atomic.Bool
	dropped atomic.Uint64
}

var _ sdklog.Processor = (*durableLogProcessor)(nil)

func newDurableLogProcessor(ctx context.Context, exporter sdklog.Exporter, cfg durableLogConfig) (*durableLogProcessor, error) {
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
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS otel_log_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at INTEGER NOT NULL,
		size_bytes INTEGER NOT NULL,
		record BLOB NOT NULL
	)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_log_queue ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `ALTER TABLE otel_log_queue ADD COLUMN created_at INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.ExecContext(ctx, `UPDATE otel_log_queue SET size_bytes = length(record) WHERE size_bytes = 0`)

	p := &durableLogProcessor{
		exporter: exporter,
		cfg:      cfg,
		db:       db,
		emitCh:   make(chan queuedLogRecord, cfg.emitQueueSize),
		wakeCh:   make(chan struct{}, 1),
		flushCh:  make(chan chan struct{}),
		done:     make(chan struct{}),
	}
	p.wg.Add(2)
	go p.writeLoop()
	go p.exportLoop()
	return p, nil
}

func (*durableLogProcessor) Enabled(context.Context, sdklog.EnabledParameters) bool {
	return true
}

func (p *durableLogProcessor) OnEmit(_ context.Context, r *sdklog.Record) error {
	if p.stopped.Load() {
		return nil
	}
	qr, err := encodeQueuedRecord(r.Clone())
	if err != nil {
		return err
	}
	select {
	case p.emitCh <- qr:
		p.wake()
	default:
		p.dropped.Add(1)
	}
	return nil
}

func (p *durableLogProcessor) ForceFlush(ctx context.Context) error {
	p.replayMu.Lock()
	defer p.replayMu.Unlock()
	if err := p.flushWriter(ctx); err != nil {
		return err
	}
	return p.drain(ctx)
}

func (p *durableLogProcessor) flushWriter(ctx context.Context) error {
	for len(p.emitCh) > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	ack := make(chan struct{})
	select {
	case p.flushCh <- ack:
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-ack:
		return nil
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *durableLogProcessor) Shutdown(ctx context.Context) error {
	if p.stopped.Swap(true) {
		return nil
	}
	close(p.done)
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		return errors.Join(ctx.Err(), p.exporter.Shutdown(ctx), p.db.Close())
	}
	err := p.drain(ctx)
	return errors.Join(err, p.exporter.Shutdown(ctx), p.db.Close())
}

func (p *durableLogProcessor) writeLoop() {
	defer p.wg.Done()
	ticker := time.NewTicker(p.cfg.writeInterval)
	defer ticker.Stop()
	batch := make([]queuedLogRecord, 0, p.cfg.writeBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := p.insert(batch); err != nil {
			otel.Handle(err)
		}
		batch = batch[:0]
		p.wake()
	}
	for {
		select {
		case <-p.done:
			for {
				select {
				case r := <-p.emitCh:
					batch = append(batch, r)
					if len(batch) >= p.cfg.writeBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case r := <-p.emitCh:
			batch = append(batch, r)
			if len(batch) >= p.cfg.writeBatchSize {
				flush()
			}
		case ack := <-p.flushCh:
			flush()
			close(ack)
		case <-ticker.C:
			flush()
		}
	}
}

func (p *durableLogProcessor) insert(records []queuedLogRecord) error {
	tx, err := p.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO otel_log_queue (created_at, size_bytes, record) VALUES (?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, r := range records {
		if _, err := stmt.Exec(r.CreatedAt, r.SizeBytes, r.Data); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return err
		}
	}
	_ = stmt.Close()
	if err := tx.Commit(); err != nil {
		return err
	}
	return p.enforceLimits()
}

func (p *durableLogProcessor) enforceLimits() error {
	_, err := p.db.Exec(`DELETE FROM otel_log_queue
		WHERE id IN (
			SELECT id FROM otel_log_queue
			ORDER BY id ASC
			LIMIT MAX((SELECT COUNT(*) FROM otel_log_queue) - ?, 0)
		)`, p.cfg.maxStoredRecords)
	if err != nil {
		return err
	}
	var totalBytes int64
	if err := p.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM otel_log_queue`).Scan(&totalBytes); err != nil {
		return err
	}
	for totalBytes > p.cfg.maxStoredBytes {
		var id int64
		var size int64
		if err := p.db.QueryRow(`SELECT id, size_bytes FROM otel_log_queue ORDER BY id ASC LIMIT 1`).Scan(&id, &size); err != nil {
			return err
		}
		if _, err := p.db.Exec(`DELETE FROM otel_log_queue WHERE id = ?`, id); err != nil {
			return err
		}
		totalBytes -= size
	}
	_, _ = p.db.Exec(`PRAGMA incremental_vacuum`)
	return err
}

func configureDurableQueueDB(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000"); err != nil {
		return err
	}
	var autoVacuum int
	if err := db.QueryRowContext(ctx, "PRAGMA auto_vacuum").Scan(&autoVacuum); err != nil {
		return err
	}
	if autoVacuum != 2 {
		if _, err := db.ExecContext(ctx, "PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
			return err
		}
	}
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return err
	}
	return nil
}

func (p *durableLogProcessor) exportLoop() {
	defer p.wg.Done()
	backoff := p.cfg.retryInitial
	timer := time.NewTimer(p.cfg.idleTimeout)
	defer timer.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-p.wakeCh:
			resetTimer(timer, p.cfg.idleTimeout)
		case <-timer.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			p.replayMu.Lock()
			err := p.exportOne(ctx)
			p.replayMu.Unlock()
			cancel()
			if err != nil {
				otel.Handle(err)
				sleep := backoff + time.Duration(rand.Int63n(int64(backoff/2+1)))
				if backoff < p.cfg.retryMax {
					backoff *= 2
					if backoff > p.cfg.retryMax {
						backoff = p.cfg.retryMax
					}
				}
				resetTimer(timer, sleep)
				continue
			}
			backoff = p.cfg.retryInitial
			if p.hasRows() {
				resetTimer(timer, 1*time.Millisecond)
			} else {
				resetTimer(timer, p.cfg.idleTimeout)
			}
		}
	}
}

func (p *durableLogProcessor) drain(ctx context.Context) error {
	for {
		if !p.hasRows() {
			return p.exporter.ForceFlush(ctx)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.exportOne(ctx); err != nil {
			return err
		}
	}
}

func (p *durableLogProcessor) exportOne(ctx context.Context) error {
	rows, err := p.db.QueryContext(ctx, `SELECT id, record, size_bytes FROM otel_log_queue ORDER BY id ASC LIMIT ?`, p.cfg.maxRecords)
	if err != nil {
		return err
	}
	defer rows.Close()

	var ids []int64
	var records []sdklog.Record
	var total int
	for rows.Next() {
		var id int64
		var data []byte
		var size int
		if err := rows.Scan(&id, &data, &size); err != nil {
			return err
		}
		if len(records) > 0 && total+size > p.cfg.maxBytes {
			break
		}
		r, err := decodeQueuedRecord(data)
		if err != nil {
			ids = append(ids, id)
			continue
		}
		ids = append(ids, id)
		records = append(records, r)
		total += size
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	if len(records) > 0 {
		if err := p.exporter.Export(ctx, records); err != nil {
			return err
		}
	}
	return p.deleteIDs(ctx, ids)
}

func (p *durableLogProcessor) deleteIDs(ctx context.Context, ids []int64) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `DELETE FROM otel_log_queue WHERE id = ?`)
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

func (p *durableLogProcessor) hasRows() bool {
	var n int
	return p.db.QueryRow(`SELECT 1 FROM otel_log_queue LIMIT 1`).Scan(&n) == nil
}

func (p *durableLogProcessor) wake() {
	select {
	case p.wakeCh <- struct{}{}:
	default:
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

type queuedLogRecord struct {
	CreatedAt int64
	SizeBytes int
	Data      []byte
}

type recordDTO struct {
	EventName         string
	Timestamp         time.Time
	ObservedTimestamp time.Time
	Severity          int
	SeverityText      string
	Body              valueDTO
	Attributes        []kvDTO
	TraceID           []byte
	SpanID            []byte
	TraceFlags        byte
	Resource          []attrDTO
	ResourceSchemaURL string
	Scope             scopeDTO
}

type scopeDTO struct {
	Name       string
	Version    string
	SchemaURL  string
	Attributes []attrDTO
}

type kvDTO struct {
	Key   string
	Value valueDTO
}

type valueDTO struct {
	Kind    int
	Bool    bool
	Float64 float64
	Int64   int64
	String  string
	Bytes   []byte
	Slice   []valueDTO
	Map     []kvDTO
}

type attrDTO struct {
	Key   string
	Type  int
	Value any
}

func encodeQueuedRecord(r sdklog.Record) (queuedLogRecord, error) {
	dto := recordDTO{
		EventName:         r.EventName(),
		Timestamp:         r.Timestamp(),
		ObservedTimestamp: r.ObservedTimestamp(),
		Severity:          int(r.Severity()),
		SeverityText:      r.SeverityText(),
		Body:              valueToDTO(r.Body()),
		TraceFlags:        byte(r.TraceFlags()),
	}
	r.WalkAttributes(func(kv otelLog.KeyValue) bool {
		dto.Attributes = append(dto.Attributes, kvDTO{Key: kv.Key, Value: valueToDTO(kv.Value)})
		return true
	})
	if tid := r.TraceID(); tid.IsValid() {
		dto.TraceID = tid[:]
	}
	if sid := r.SpanID(); sid.IsValid() {
		dto.SpanID = sid[:]
	}
	if res := r.Resource(); res != nil {
		dto.ResourceSchemaURL = res.SchemaURL()
		iter := res.Iter()
		for iter.Next() {
			dto.Resource = append(dto.Resource, attrToDTO(iter.Attribute()))
		}
	}
	scope := r.InstrumentationScope()
	dto.Scope.Name = scope.Name
	dto.Scope.Version = scope.Version
	dto.Scope.SchemaURL = scope.SchemaURL
	iter := scope.Attributes.Iter()
	for iter.Next() {
		dto.Scope.Attributes = append(dto.Scope.Attributes, attrToDTO(iter.Attribute()))
	}
	data, err := json.Marshal(dto)
	if err != nil {
		return queuedLogRecord{}, err
	}
	return queuedLogRecord{CreatedAt: time.Now().UnixNano(), SizeBytes: len(data), Data: data}, nil
}

func decodeQueuedRecord(data []byte) (sdklog.Record, error) {
	var dto recordDTO
	if err := json.Unmarshal(data, &dto); err != nil {
		return sdklog.Record{}, err
	}
	r := sdklog.Record{}
	setRecordField(&r, "attributeCountLimit", -1)
	setRecordField(&r, "attributeValueLengthLimit", -1)
	r.SetEventName(dto.EventName)
	r.SetTimestamp(dto.Timestamp)
	r.SetObservedTimestamp(dto.ObservedTimestamp)
	r.SetSeverity(otelLog.Severity(dto.Severity))
	r.SetSeverityText(dto.SeverityText)
	r.SetBody(dto.Body.toValue())
	attrs := make([]otelLog.KeyValue, 0, len(dto.Attributes))
	for _, kv := range dto.Attributes {
		attrs = append(attrs, otelLog.KeyValue{Key: kv.Key, Value: kv.Value.toValue()})
	}
	r.SetAttributes(attrs...)
	if len(dto.TraceID) == 16 {
		var tid trace.TraceID
		copy(tid[:], dto.TraceID)
		r.SetTraceID(tid)
	}
	if len(dto.SpanID) == 8 {
		var sid trace.SpanID
		copy(sid[:], dto.SpanID)
		r.SetSpanID(sid)
	}
	r.SetTraceFlags(trace.TraceFlags(dto.TraceFlags))
	setRecordField(&r, "resource", resource.NewWithAttributes(dto.ResourceSchemaURL, attrsFromDTO(dto.Resource)...))
	scopeAttrs := attribute.NewSet(attrsFromDTO(dto.Scope.Attributes)...)
	setRecordField(&r, "scope", &instrumentation.Scope{
		Name:       dto.Scope.Name,
		Version:    dto.Scope.Version,
		SchemaURL:  dto.Scope.SchemaURL,
		Attributes: scopeAttrs,
	})
	return r, nil
}

func valueToDTO(v otelLog.Value) valueDTO {
	dto := valueDTO{Kind: int(v.Kind())}
	switch v.Kind() {
	case otelLog.KindBool:
		dto.Bool = v.AsBool()
	case otelLog.KindFloat64:
		dto.Float64 = v.AsFloat64()
	case otelLog.KindInt64:
		dto.Int64 = v.AsInt64()
	case otelLog.KindString:
		dto.String = v.AsString()
	case otelLog.KindBytes:
		dto.Bytes = append([]byte(nil), v.AsBytes()...)
	case otelLog.KindSlice:
		for _, item := range v.AsSlice() {
			dto.Slice = append(dto.Slice, valueToDTO(item))
		}
	case otelLog.KindMap:
		for _, kv := range v.AsMap() {
			dto.Map = append(dto.Map, kvDTO{Key: kv.Key, Value: valueToDTO(kv.Value)})
		}
	}
	return dto
}

func (v valueDTO) toValue() otelLog.Value {
	switch otelLog.Kind(v.Kind) {
	case otelLog.KindBool:
		return otelLog.BoolValue(v.Bool)
	case otelLog.KindFloat64:
		return otelLog.Float64Value(v.Float64)
	case otelLog.KindInt64:
		return otelLog.Int64Value(v.Int64)
	case otelLog.KindString:
		return otelLog.StringValue(v.String)
	case otelLog.KindBytes:
		return otelLog.BytesValue(v.Bytes)
	case otelLog.KindSlice:
		items := make([]otelLog.Value, 0, len(v.Slice))
		for _, item := range v.Slice {
			items = append(items, item.toValue())
		}
		return otelLog.SliceValue(items...)
	case otelLog.KindMap:
		items := make([]otelLog.KeyValue, 0, len(v.Map))
		for _, item := range v.Map {
			items = append(items, otelLog.KeyValue{Key: item.Key, Value: item.Value.toValue()})
		}
		return otelLog.MapValue(items...)
	default:
		return otelLog.Value{}
	}
}

func attrToDTO(kv attribute.KeyValue) attrDTO {
	return attrDTO{Key: string(kv.Key), Type: int(kv.Value.Type()), Value: kv.Value.AsInterface()}
}

func attrsFromDTO(in []attrDTO) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(in))
	for _, kv := range in {
		key := attribute.Key(kv.Key)
		switch attribute.Type(kv.Type) {
		case attribute.BOOL:
			if v, ok := kv.Value.(bool); ok {
				out = append(out, key.Bool(v))
			}
		case attribute.INT64:
			if v, ok := asFloat(kv.Value); ok {
				out = append(out, key.Int64(int64(v)))
			}
		case attribute.FLOAT64:
			if v, ok := asFloat(kv.Value); ok {
				out = append(out, key.Float64(v))
			}
		case attribute.STRING:
			if v, ok := kv.Value.(string); ok {
				out = append(out, key.String(v))
			}
		case attribute.BOOLSLICE:
			if v, ok := boolSliceFromAny(kv.Value); ok {
				out = append(out, key.BoolSlice(v))
			}
		case attribute.INT64SLICE:
			if v, ok := int64SliceFromAny(kv.Value); ok {
				out = append(out, key.Int64Slice(v))
			}
		case attribute.FLOAT64SLICE:
			if v, ok := float64SliceFromAny(kv.Value); ok {
				out = append(out, key.Float64Slice(v))
			}
		case attribute.STRINGSLICE:
			if v, ok := stringSliceFromAny(kv.Value); ok {
				out = append(out, key.StringSlice(v))
			}
		}
	}
	return out
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

func boolSliceFromAny(v any) ([]bool, bool) {
	switch t := v.(type) {
	case []bool:
		return append([]bool(nil), t...), true
	case []interface{}:
		out := make([]bool, 0, len(t))
		for _, item := range t {
			b, ok := item.(bool)
			if !ok {
				return nil, false
			}
			out = append(out, b)
		}
		return out, true
	default:
		return nil, false
	}
}

func int64SliceFromAny(v any) ([]int64, bool) {
	switch t := v.(type) {
	case []int64:
		return append([]int64(nil), t...), true
	case []interface{}:
		out := make([]int64, 0, len(t))
		for _, item := range t {
			n, ok := asFloat(item)
			if !ok {
				return nil, false
			}
			out = append(out, int64(n))
		}
		return out, true
	default:
		return nil, false
	}
}

func float64SliceFromAny(v any) ([]float64, bool) {
	switch t := v.(type) {
	case []float64:
		return append([]float64(nil), t...), true
	case []interface{}:
		out := make([]float64, 0, len(t))
		for _, item := range t {
			n, ok := asFloat(item)
			if !ok {
				return nil, false
			}
			out = append(out, n)
		}
		return out, true
	default:
		return nil, false
	}
}

func stringSliceFromAny(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...), true
	case []interface{}:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	default:
		return nil, false
	}
}

func setRecordField(r *sdklog.Record, name string, value any) {
	rVal := reflect.ValueOf(r).Elem()
	rf := rVal.FieldByName(name)
	rf = reflect.NewAt(rf.Type(), unsafe.Pointer(rf.UnsafeAddr())).Elem()
	rf.Set(reflect.ValueOf(value))
}
