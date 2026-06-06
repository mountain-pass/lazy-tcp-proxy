package metrics

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// ServiceActivity holds the weekly usage heatmap for one (container_name, port, is_udp) tuple.
// Each day field is a 24-element array indexed by hour-of-day (0–23).
// Cell values: 0 = no activity (no uptime); 1 = active (uptime_ms_total > 0
// but no connections started); 2 = active with connections (uptime_ms_total > 0
// and at least one connection started), based on proxy_metrics rows from the
// last 7 days for that weekday+hour.
// Active is true if any cell across all seven days is non-zero.
type ServiceActivity struct {
	ContainerName string   `json:"container_name"`
	Port          int      `json:"port"`
	IsUDP         bool     `json:"is_udp"`
	Mon           [24]int8 `json:"mon"`
	Tue           [24]int8 `json:"tue"`
	Wed           [24]int8 `json:"wed"`
	Thu           [24]int8 `json:"thu"`
	Fri           [24]int8 `json:"fri"`
	Sat           [24]int8 `json:"sat"`
	Sun           [24]int8 `json:"sun"`
	Active        bool     `json:"active"`
}

// Snapshot is one minute of data for a single port, written as a row in proxy_metrics.
type Snapshot struct {
	RollupAt             time.Time
	ContainerName        string
	Port                 int
	IsUDP                bool
	Availability         string
	ConnectionsStarted   int64
	ConnectionsEnded     int64
	ConnectionsActive    int32
	ConnectionsPeak      int32
	ConnectionsFailed    int64
	BytesSent            int64
	BytesReceived        int64
	RequestDurationMsAvg *float64
	RequestDurationMsMax *int64
	RequestDurationMsMin *int64
	ContainerStarts      int64
	ColdStartMsAvg       *float64
	ColdStartMsMax       *int64
	ContainerStops       int64
	UptimeMsTotal        int64
}

type portKey struct {
	port  int
	isUDP bool
}

type portAccumulator struct {
	containerName string
	port          int
	isUDP         bool
	availability  string

	// Atomic counters reset to zero on each rollup.
	connectionsStarted atomic.Int64
	connectionsEnded   atomic.Int64
	connectionsFailed  atomic.Int64
	bytesSent          atomic.Int64
	bytesReceived      atomic.Int64
	containerStarts    atomic.Int64
	containerStops     atomic.Int64

	// activeConns is a gauge: incremented on start, decremented on end, never reset.
	activeConns atomic.Int32
	// peakConns is the high-water mark of activeConns during the window; reset on rollup.
	peakConns atomic.Int32

	// mu protects all fields below.
	mu sync.Mutex

	durationCount   int64
	durationTotalMs int64
	durationMaxMs   int64
	durationMinMs   int64 // math.MaxInt64 when no samples

	coldStartCount   int64
	coldStartTotalMs int64
	coldStartMaxMs   int64

	containerRunning bool
	containerUpAt    time.Time
	uptimeAccumMs    int64

	windowStart time.Time
}

// Collector gathers per-port metrics and flushes them to PostgreSQL every minute.
type Collector struct {
	mu    sync.RWMutex
	ports map[portKey]*portAccumulator
	db    *postgresDB
	retry retryBuffer
}

// New connects to PostgreSQL, ensures the schema exists, and returns a ready Collector.
func New(ctx context.Context, pgURL string) (*Collector, error) {
	db, err := newPostgresDB(ctx, pgURL)
	if err != nil {
		return nil, err
	}
	return &Collector{
		ports: make(map[portKey]*portAccumulator),
		db:    db,
	}, nil
}

// RegisterPort creates or updates metadata for a port mapping.
// Safe to call multiple times for the same port (updates metadata, keeps accumulators).
func (c *Collector) RegisterPort(port int, containerName string, isUDP bool, availability string) {
	k := portKey{port, isUDP}
	c.mu.Lock()
	defer c.mu.Unlock()
	if a, ok := c.ports[k]; ok {
		a.containerName = containerName
		a.availability = availability
		return
	}
	c.ports[k] = &portAccumulator{
		containerName: containerName,
		port:          port,
		isUDP:         isUDP,
		availability:  availability,
		durationMinMs: math.MaxInt64,
		windowStart:   time.Now(),
	}
}

// UnregisterPort removes a port mapping. In-flight accumulator data is discarded.
func (c *Collector) UnregisterPort(port int, isUDP bool) {
	c.mu.Lock()
	delete(c.ports, portKey{port, isUDP})
	c.mu.Unlock()
}

func (c *Collector) get(port int, isUDP bool) *portAccumulator {
	c.mu.RLock()
	a := c.ports[portKey{port, isUDP}]
	c.mu.RUnlock()
	return a
}

// ConnectionStarted records a new inbound connection or UDP flow.
func (c *Collector) ConnectionStarted(port int, isUDP bool) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.connectionsStarted.Add(1)
	newActive := a.activeConns.Add(1)
	for {
		cur := a.peakConns.Load()
		if newActive <= cur {
			break
		}
		if a.peakConns.CompareAndSwap(cur, newActive) {
			break
		}
	}
}

// ConnectionEnded records a closed connection or flow with its duration.
func (c *Collector) ConnectionEnded(port int, isUDP bool, durationMs int64) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.connectionsEnded.Add(1)
	a.activeConns.Add(-1)

	a.mu.Lock()
	a.durationCount++
	a.durationTotalMs += durationMs
	if durationMs > a.durationMaxMs {
		a.durationMaxMs = durationMs
	}
	if durationMs < a.durationMinMs {
		a.durationMinMs = durationMs
	}
	a.mu.Unlock()
}

// ConnectionFailed records a connection that was attempted but could not be established.
// Must be called without a prior ConnectionStarted (failed before any flow existed).
func (c *Collector) ConnectionFailed(port int, isUDP bool) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.connectionsFailed.Add(1)
}

// AddBytes records bytes transferred for a port in either direction.
func (c *Collector) AddBytes(port int, isUDP bool, sent, recv int64) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	if sent > 0 {
		a.bytesSent.Add(sent)
	}
	if recv > 0 {
		a.bytesReceived.Add(recv)
	}
}

// RecordColdStart records a connection-triggered container cold start (called by the
// singleflight winner goroutine only, i.e. !shared).
func (c *Collector) RecordColdStart(port int, isUDP bool, durationMs int64) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.containerStarts.Add(1)
	a.mu.Lock()
	a.coldStartCount++
	a.coldStartTotalMs += durationMs
	if durationMs > a.coldStartMaxMs {
		a.coldStartMaxMs = durationMs
	}
	a.mu.Unlock()
}

// RecordContainerStart records a container start triggered by cron or manual schedule
// (no cold-start duration is available in these cases).
func (c *Collector) RecordContainerStart(port int, isUDP bool) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.containerStarts.Add(1)
}

// OnContainerRunning marks a port's upstream container as running for uptime tracking.
// Safe to call multiple times; subsequent calls while already running are no-ops.
func (c *Collector) OnContainerRunning(port int, isUDP bool, at time.Time) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.mu.Lock()
	if !a.containerRunning {
		a.containerRunning = true
		a.containerUpAt = at
	}
	a.mu.Unlock()
}

// ContainerStopped records a container stop and updates the uptime accumulator.
func (c *Collector) ContainerStopped(port int, isUDP bool) {
	a := c.get(port, isUDP)
	if a == nil {
		return
	}
	a.containerStops.Add(1)
	now := time.Now()
	a.mu.Lock()
	if a.containerRunning {
		a.uptimeAccumMs += now.Sub(a.containerUpAt).Milliseconds()
		a.containerRunning = false
	}
	a.mu.Unlock()
}

// HourlyActivity queries the last 7 days of metrics and returns one entry per
// (container_name, port, is_udp) with pre-aggregated day-of-week hour arrays.
func (c *Collector) HourlyActivity(ctx context.Context) ([]ServiceActivity, error) {
	return c.db.hourlyActivity(ctx)
}

// Run starts the 1-minute rollup loop. Blocks until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) {
	defer c.db.close()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			windowEnd := time.Now()
			snaps := c.rollupAll(windowEnd)
			go c.flush(ctx, snaps)
		}
	}
}

func (c *Collector) rollupAll(windowEnd time.Time) []Snapshot {
	c.mu.RLock()
	accumulators := make([]*portAccumulator, 0, len(c.ports))
	for _, a := range c.ports {
		accumulators = append(accumulators, a)
	}
	c.mu.RUnlock()

	snaps := make([]Snapshot, 0, len(accumulators))
	for _, a := range accumulators {
		snaps = append(snaps, a.rollup(windowEnd))
	}
	return snaps
}

func (a *portAccumulator) rollup(windowEnd time.Time) Snapshot {
	started := a.connectionsStarted.Swap(0)
	ended   := a.connectionsEnded.Swap(0)
	failed  := a.connectionsFailed.Swap(0)
	sent    := a.bytesSent.Swap(0)
	recv    := a.bytesReceived.Swap(0)
	cStart  := a.containerStarts.Swap(0)
	cStop   := a.containerStops.Swap(0)
	active  := a.activeConns.Load() // gauge: not reset
	// Seed the new window's peak with the current active count so long-lived
	// connections spanning multiple rollup windows are reflected in peak.
	peak := a.peakConns.Swap(active)
	if peak < active {
		peak = active
	}

	a.mu.Lock()

	// Uptime: if container is currently running, add elapsed time up to windowEnd,
	// then reset containerUpAt to windowEnd so the next window starts cleanly (no gap).
	uptime := a.uptimeAccumMs
	if a.containerRunning {
		uptime += windowEnd.Sub(a.containerUpAt).Milliseconds()
		a.containerUpAt = windowEnd
	}
	a.uptimeAccumMs = 0

	snap := Snapshot{
		RollupAt:           a.windowStart,
		ContainerName:      a.containerName,
		Port:               a.port,
		IsUDP:              a.isUDP,
		Availability:       a.availability,
		ConnectionsStarted: started,
		ConnectionsEnded:   ended,
		ConnectionsFailed:  failed,
		ConnectionsActive:  active,
		ConnectionsPeak:    peak,
		BytesSent:          sent,
		BytesReceived:      recv,
		ContainerStarts:    cStart,
		ContainerStops:     cStop,
		UptimeMsTotal:      uptime,
	}

	if a.durationCount > 0 {
		avg := float64(a.durationTotalMs) / float64(a.durationCount)
		maxD := a.durationMaxMs
		minD := a.durationMinMs
		snap.RequestDurationMsAvg = &avg
		snap.RequestDurationMsMax = &maxD
		snap.RequestDurationMsMin = &minD
	}
	a.durationCount, a.durationTotalMs, a.durationMaxMs, a.durationMinMs = 0, 0, 0, math.MaxInt64

	if a.coldStartCount > 0 {
		avg := float64(a.coldStartTotalMs) / float64(a.coldStartCount)
		maxCS := a.coldStartMaxMs
		snap.ColdStartMsAvg = &avg
		snap.ColdStartMsMax = &maxCS
	}
	a.coldStartCount, a.coldStartTotalMs, a.coldStartMaxMs = 0, 0, 0

	a.windowStart = windowEnd // advance window — contiguous, no gap
	a.mu.Unlock()

	return snap
}
