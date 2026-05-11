package syncbench

import (
	"sync"
	"time"
)

type Method string

const (
	MethodWebSocket Method = "ws_subscribe_newHeads"
	MethodPolling   Method = "http_poll_blockNumber"
)

type BlockEvent struct {
	BlockNumber    uint64
	ObservedAt     time.Time
	BlockTimestamp time.Time
}

type RPCStats struct {
	Calls    int
	BytesIn  int64
	BytesOut int64
}

type Run struct {
	Method       Method
	PollInterval time.Duration // 0 for WS

	mu     sync.Mutex
	Events []BlockEvent
	Stats  RPCStats
}

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

func (r *Run) snapshot() ([]BlockEvent, RPCStats) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]BlockEvent, len(r.Events))
	copy(out, r.Events)
	return out, r.Stats
}

type CUCosts struct {
	BlockNumber             int
	SubscribeNewHeadsPerMsg int
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
	HTTPRPS       float64
	MaxRetries    int
}
