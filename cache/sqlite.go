package cache

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/vmihailenco/msgpack/v5"
	_ "modernc.org/sqlite"
)

type sqliteCache struct {
	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	once   sync.Once
	cfg    config
}

var _ Cache = (*sqliteCache)(nil)

// queryCtx returns a context with the per-operation timeout derived from the
// given parent context.
func (c *sqliteCache) queryCtx(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, c.cfg.queryTimeout)
}

// NewSQLite returns a new Cache backed by SQLite.
// If dbPath is empty or ":memory:", an in-memory database is used.
func NewSQLite(ctx context.Context, dbPath string, opts ...Option) (Cache, error) {
	cfg := applyOptions(opts)
	if dbPath == "" {
		dbPath = ":memory:"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS cache (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL,
		expires_at INTEGER NOT NULL,
		hits INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_cache_expires_at ON cache(expires_at)`); err != nil {
		db.Close()
		return nil, err
	}

	childCtx, cancel := context.WithCancel(ctx)
	c := &sqliteCache{
		db:     db,
		ctx:    childCtx,
		cancel: cancel,
		cfg:    cfg,
	}
	c.wg.Add(1)
	go c.run()
	return c, nil
}

func (c *sqliteCache) GetContext(ctx context.Context, key string) (bool, any, error) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	now := time.Now().UnixNano()
	var data []byte
	var expiresAt int64
	err := c.db.QueryRowContext(qctx,
		`SELECT value, expires_at FROM cache WHERE key = ?`, key,
	).Scan(&data, &expiresAt)
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}

	// Check if expired.
	if expiresAt < now {
		// Lazily delete expired entry.
		_, _ = c.db.ExecContext(qctx, `DELETE FROM cache WHERE key = ?`, key)
		return false, nil, nil
	}

	// Increment hits.
	_, _ = c.db.ExecContext(qctx, `UPDATE cache SET hits = hits + 1 WHERE key = ?`, key)

	return true, data, nil
}

func (c *sqliteCache) Get(key string) (bool, any, error) {
	return c.GetContext(c.ctx, key)
}

func (c *sqliteCache) SetContext(ctx context.Context, key string, val any, expires time.Duration) error {
	if expires <= 0 {
		expires = c.cfg.defaultExpires
	}
	data, err := msgpack.Marshal(val)
	if err != nil {
		return err
	}
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	expiresAt := time.Now().Add(expires).UnixNano()
	_, err = c.db.ExecContext(qctx,
		`INSERT INTO cache (key, value, expires_at, hits) VALUES (?, ?, ?, 0)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at, hits = 0`,
		key, data, expiresAt,
	)
	return err
}

func (c *sqliteCache) Set(key string, val any, expires time.Duration) error {
	return c.SetContext(c.ctx, key, val, expires)
}

func (c *sqliteCache) HitsContext(ctx context.Context, key string) (bool, int) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	var hits int
	err := c.db.QueryRowContext(qctx, `SELECT hits FROM cache WHERE key = ?`, key).Scan(&hits)
	if err != nil {
		return false, 0
	}
	return true, hits
}

func (c *sqliteCache) Hits(key string) (bool, int) {
	return c.HitsContext(c.ctx, key)
}

func (c *sqliteCache) ExpireContext(ctx context.Context, key string) (bool, error) {
	qctx, cancel := c.queryCtx(ctx)
	defer cancel()
	result, err := c.db.ExecContext(qctx, `DELETE FROM cache WHERE key = ?`, key)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (c *sqliteCache) Expire(key string) (bool, error) {
	return c.ExpireContext(c.ctx, key)
}

func (c *sqliteCache) CloseContext(_ context.Context) error {
	var dbErr error
	c.once.Do(func() {
		c.cancel()
		c.wg.Wait()
		dbErr = c.db.Close()
	})
	return dbErr
}

func (c *sqliteCache) Close() error {
	return c.CloseContext(c.ctx)
}

func (c *sqliteCache) run() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.cfg.expiryCheck)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := c.queryCtx(c.ctx)
			now := time.Now().UnixNano()
			_, _ = c.db.ExecContext(ctx, `DELETE FROM cache WHERE expires_at < ?`, now)
			cancel()
		}
	}
}
