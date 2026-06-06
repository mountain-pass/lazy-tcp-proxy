package metrics

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const createSchema = `
CREATE TABLE IF NOT EXISTS proxy_metrics (
    id                      BIGSERIAL PRIMARY KEY,
    rollup_at               TIMESTAMPTZ NOT NULL,
    container_name          TEXT NOT NULL,
    port                    INTEGER NOT NULL,
    is_udp                  BOOLEAN NOT NULL DEFAULT FALSE,
    availability            TEXT NOT NULL,
    connections_started     BIGINT NOT NULL DEFAULT 0,
    connections_ended       BIGINT NOT NULL DEFAULT 0,
    connections_active      INTEGER NOT NULL DEFAULT 0,
    connections_peak        INTEGER NOT NULL DEFAULT 0,
    connections_failed      BIGINT NOT NULL DEFAULT 0,
    bytes_sent              BIGINT NOT NULL DEFAULT 0,
    bytes_received          BIGINT NOT NULL DEFAULT 0,
    request_duration_ms_avg DOUBLE PRECISION,
    request_duration_ms_max BIGINT,
    request_duration_ms_min BIGINT,
    container_starts        BIGINT NOT NULL DEFAULT 0,
    cold_start_ms_avg       DOUBLE PRECISION,
    cold_start_ms_max       BIGINT,
    container_stops         BIGINT NOT NULL DEFAULT 0,
    uptime_ms_total         BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS proxy_metrics_rollup_at ON proxy_metrics (rollup_at);
CREATE INDEX IF NOT EXISTS proxy_metrics_container  ON proxy_metrics (container_name, port, rollup_at);
`

const insertRow = `
INSERT INTO proxy_metrics (
    rollup_at, container_name, port, is_udp, availability,
    connections_started, connections_ended, connections_active,
    connections_peak, connections_failed,
    bytes_sent, bytes_received,
    request_duration_ms_avg, request_duration_ms_max, request_duration_ms_min,
    container_starts, cold_start_ms_avg, cold_start_ms_max,
    container_stops, uptime_ms_total
) VALUES (
    $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
)`

type postgresDB struct {
	pool *pgxpool.Pool
}

func newPostgresDB(ctx context.Context, pgURL string) (*postgresDB, error) {
	pool, err := pgxpool.New(ctx, pgURL)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := pool.Exec(ctx, createSchema); err != nil {
		pool.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &postgresDB{pool: pool}, nil
}

func (db *postgresDB) write(ctx context.Context, s Snapshot) error {
	writeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := db.pool.Exec(writeCtx, insertRow,
		s.RollupAt, s.ContainerName, s.Port, s.IsUDP, s.Availability,
		s.ConnectionsStarted, s.ConnectionsEnded, s.ConnectionsActive,
		s.ConnectionsPeak, s.ConnectionsFailed,
		s.BytesSent, s.BytesReceived,
		s.RequestDurationMsAvg, s.RequestDurationMsMax, s.RequestDurationMsMin,
		s.ContainerStarts, s.ColdStartMsAvg, s.ColdStartMsMax,
		s.ContainerStops, s.UptimeMsTotal,
	)
	return err
}

func (db *postgresDB) close() {
	db.pool.Close()
}

const queryHourlyActivity = `
SELECT container_name, port, is_udp,
       EXTRACT(DOW  FROM rollup_at)::int AS dow,
       EXTRACT(HOUR FROM rollup_at)::int AS hr,
       SUM(connections_peak) AS conns
FROM proxy_metrics
WHERE rollup_at >= NOW() - INTERVAL '7 days'
  AND uptime_ms_total > 0
GROUP BY container_name, port, is_udp, dow, hr
ORDER BY container_name, port, is_udp, dow, hr
`

type serviceKey struct {
	containerName string
	port          int
	isUDP         bool
}

// hourlyActivity returns one ServiceActivity per (container_name, port, is_udp) tuple.
// DOW values: 0=Sunday, 1=Monday, 2=Tuesday, 3=Wednesday, 4=Thursday, 5=Friday, 6=Saturday.
func (db *postgresDB) hourlyActivity(ctx context.Context) ([]ServiceActivity, error) {
	rows, err := db.pool.Query(ctx, queryHourlyActivity)
	if err != nil {
		return nil, fmt.Errorf("hourlyActivity query: %w", err)
	}
	defer rows.Close()

	index := make(map[serviceKey]*ServiceActivity)
	var order []serviceKey

	for rows.Next() {
		var (
			name  string
			port  int
			isUDP bool
			dow   int
			hr    int
			conns int64
		)
		if err := rows.Scan(&name, &port, &isUDP, &dow, &hr, &conns); err != nil {
			return nil, fmt.Errorf("hourlyActivity scan: %w", err)
		}
		k := serviceKey{name, port, isUDP}
		sa, ok := index[k]
		if !ok {
			sa = &ServiceActivity{ContainerName: name, Port: port, IsUDP: isUDP}
			index[k] = sa
			order = append(order, k)
		}
		if hr >= 0 && hr < 24 {
			val := int8(1)
			if conns > 0 {
				val = 2
			}
			switch dow {
			case 0:
				sa.Sun[hr] = val
			case 1:
				sa.Mon[hr] = val
			case 2:
				sa.Tue[hr] = val
			case 3:
				sa.Wed[hr] = val
			case 4:
				sa.Thu[hr] = val
			case 5:
				sa.Fri[hr] = val
			case 6:
				sa.Sat[hr] = val
			}
			sa.Active = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]ServiceActivity, 0, len(order))
	for _, k := range order {
		out = append(out, *index[k])
	}
	return out, nil
}

// retryBuffer holds up to 5 failed snapshots, oldest first.
// When full, push drops the oldest to make room for the newest.
type retryBuffer struct {
	mu  sync.Mutex
	buf []*Snapshot
}

func (rb *retryBuffer) push(s *Snapshot) {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	if len(rb.buf) >= 5 {
		rb.buf = rb.buf[1:] // drop oldest
	}
	rb.buf = append(rb.buf, s)
}

func (rb *retryBuffer) drain() []*Snapshot {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := rb.buf
	rb.buf = nil
	return out
}

func (rb *retryBuffer) len() int {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	return len(rb.buf)
}

// flush writes buffered snapshots (oldest first) then the current batch.
// Each snapshot that fails to write is pushed back into the retry buffer.
func (c *Collector) flush(ctx context.Context, current []Snapshot) {
	pending := c.retry.drain()
	for _, s := range pending {
		if err := c.db.write(ctx, *s); err != nil {
			c.retry.push(s)
			log.Printf("metrics: write failed (buffered %d/5): %v", c.retry.len(), err)
		}
	}
	for i := range current {
		if err := c.db.write(ctx, current[i]); err != nil {
			c.retry.push(&current[i])
			log.Printf("metrics: write failed (buffered %d/5): %v", c.retry.len(), err)
		}
	}
}
