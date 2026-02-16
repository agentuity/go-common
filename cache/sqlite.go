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
	db          *sql.DB
	ctx         context.Context
	cancel      context.CancelFunc
	waitGroup   sync.WaitGroup
	once        sync.Once
	expiryCheck time.Duration
}

var _ Cache = (*sqliteCache)(nil)

// NewSQLite returns a new Cache backed by SQLite.
// If dbPath is empty or ":memory:", an in-memory database is used.
// expiryCheck controls how often expired entries are cleaned up.
func NewSQLite(ctx context.Context, dbPath string, expiryCheck time.Duration) (Cache, error) {
	if dbPath == "" {
		dbPath = ":memory:"
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Enable WAL mode for better concurrent performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	// Create the cache table.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS cache (
		key TEXT PRIMARY KEY,
		value BLOB NOT NULL,
		expires_at INTEGER NOT NULL,
		hits INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}

	// Create index on expires_at for efficient cleanup.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_cache_expires_at ON cache(expires_at)`); err != nil {
		db.Close()
		return nil, err
	}

	childCtx, cancel := context.WithCancel(ctx)

	c := &sqliteCache{
		db:          db,
		ctx:         childCtx,
		cancel:      cancel,
		expiryCheck: expiryCheck,
	}
	if c.expiryCheck <= 0 {
		c.expiryCheck = time.Minute
	}

	c.waitGroup.Add(1)
	go c.run()

	return c, nil
}

func (c *sqliteCache) Get(key string) (bool, any, error) {
	now := time.Now().UnixNano()
	var data []byte
	var expiresAt int64
	err := c.db.QueryRow(
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
		_, _ = c.db.Exec(`DELETE FROM cache WHERE key = ?`, key)
		return false, nil, nil
	}

	// Increment hits.
	_, _ = c.db.Exec(`UPDATE cache SET hits = hits + 1 WHERE key = ?`, key)

	return true, data, nil
}

func (c *sqliteCache) Set(key string, val any, expires time.Duration) error {
	data, err := msgpack.Marshal(val)
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(expires).UnixNano()
	_, err = c.db.Exec(
		`INSERT INTO cache (key, value, expires_at, hits) VALUES (?, ?, ?, 0)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, expires_at = excluded.expires_at, hits = 0`,
		key, data, expiresAt,
	)
	return err
}

func (c *sqliteCache) Hits(key string) (bool, int) {
	var hits int
	err := c.db.QueryRow(`SELECT hits FROM cache WHERE key = ?`, key).Scan(&hits)
	if err != nil {
		return false, 0
	}
	return true, hits
}

func (c *sqliteCache) Expire(key string) (bool, error) {
	result, err := c.db.Exec(`DELETE FROM cache WHERE key = ?`, key)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (c *sqliteCache) Close() error {
	var dbErr error
	c.once.Do(func() {
		c.cancel()
		c.waitGroup.Wait()
		dbErr = c.db.Close()
	})
	return dbErr
}

func (c *sqliteCache) run() {
	defer c.waitGroup.Done()
	ticker := time.NewTicker(c.expiryCheck)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UnixNano()
			_, _ = c.db.Exec(`DELETE FROM cache WHERE expires_at < ?`, now)
		}
	}
}
