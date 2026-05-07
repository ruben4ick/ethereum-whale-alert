package syncbench

import (
	"sync"
	"time"
)

type Method string

const (
	// MethodWebSocket — push delivery via eth_subscribe newHeads.
	MethodWebSocket Method = "ws_subscribe_newHeads"
	// MethodPolling — pull delivery via periodic eth_blockNumber over HTTP.
	// Each poll interval is treated as a distinct method variant in the report.
	MethodPolling Method = "http_poll_blockNumber"
)

// BlockEvent records that a method observed a particular block height for the
// first time. Different methods will produce one BlockEvent per block they see;
// for a long polling interval one poll may emit several BlockEvents (one per
// block in the gap) — all sharing the same ObservedAt, since the poller cannot
// know the intermediate arrival times.
type BlockEvent struct {
	BlockNumber uint64
	// ObservedAt is the wall-clock time the method delivered the event to the
	// application (after WS frame parse / HTTP response).
	ObservedAt time.Time
	// BlockTimestamp comes from the block header (set by the proposer); only
	// populated for methods that carry header data — i.e. WebSocket. For
	// polling we leave it zero and resolve it post-hoc from the WS run.
	BlockTimestamp time.Time
}

// RPCStats accumulates wire-level stats over the whole run for one method.
// WebSocket bytes are not measured at frame level (we use go-ethereum's
// rpc.Client which hides framing); BytesIn/Out remain 0 for WS and we report
// only call counts there.
type RPCStats struct {
	Calls    int
	BytesIn  int64
	BytesOut int64
}

// Run is the per-method log of observations and RPC stats. It is appended to
// concurrently from a single goroutine per method; the mutex guards against
// later aggregation racing with the writer if Stop is invoked early.
type Run struct {
	Method       Method
	PollInterval time.Duration // 0 for WS

	mu     sync.Mutex
	Events []BlockEvent
	Stats  RPCStats
}

// Label returns a human-readable identifier suitable for CSV / report rows.
// "ws_subscribe_newHeads" or "http_poll_blockNumber@3s".
func (r *Run) Label() string {
	if r.Method == MethodPolling {
		return string(r.Method) + "@" + r.PollInterval.String()
	}
	return string(r.Method)
}

func (r *Run) addEvent(e BlockEvent) {
	r.mu.Lock()
	r.Events = append(r.Events, e)
	r.mu.Unlock()
}

func (r *Run) addStats(calls int, bytesIn, bytesOut int64) {
	r.mu.Lock()
	r.Stats.Calls += calls
	r.Stats.BytesIn += bytesIn
	r.Stats.BytesOut += bytesOut
	r.mu.Unlock()
}

// snapshot returns a copy of the events slice and stats safe to read after the
// run goroutine has exited.
func (r *Run) snapshot() ([]BlockEvent, RPCStats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]BlockEvent, len(r.Events))
	copy(out, r.Events)
	return out, r.Stats
}

// CUCosts models the provider's billing for the operations used here. Defaults
// match Alchemy's published rates; override via env if running against a
// different provider.
type CUCosts struct {
	BlockNumber             int // per HTTP eth_blockNumber call
	SubscribeNewHeadsPerMsg int // per WS newHeads message delivered
}

func DefaultAlchemyCU() CUCosts {
	return CUCosts{
		BlockNumber:             10,
		SubscribeNewHeadsPerMsg: 20,
	}
}

type Config struct {
	WSURL         string
	HTTPURL       string
	Duration      time.Duration
	PollIntervals []time.Duration
	CU            CUCosts
	// HTTPRPS is the rate-limit budget applied to all pollers combined to keep
	// us within provider limits; 0 disables limiting.
	HTTPRPS    float64
	MaxRetries int
}
