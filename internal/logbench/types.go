package logbench

import "time"

type Method string

const (
	MethodGetLogsPerBlock        Method = "eth_getLogs (per block)"
	MethodGetLogsRanged          Method = "eth_getLogs (ranged)"
	MethodGetBlockReceipts       Method = "eth_getBlockReceipts"
	MethodGetTransactionReceipts Method = "eth_getTransactionReceipt (per tx)"
)

// BlockMeasurement is one measurement of a single method against a single block
// (or the smallest unit of work the method operates on — a range chunk for ranged getLogs).
type BlockMeasurement struct {
	Method      Method
	BlockNumber uint64
	RPCCalls    int
	BytesIn     int64 // total response payload bytes for this block
	BytesOut    int64 // total request payload bytes for this block
	Latency     time.Duration
	LogCount    int
	TxCount     int
	Error       string
}

// MethodResult aggregates all per-block measurements for a single method.
type MethodResult struct {
	Method        Method
	Blocks        int
	TotalRPCCalls int
	TotalBytesIn  int64
	TotalBytesOut int64
	TotalWall     time.Duration // wall-clock time of the whole run, with chosen concurrency
	TotalCallTime time.Duration // sum of per-call latencies (ignores concurrency)
	AvgLatency    time.Duration // mean per-call latency
	P50Latency    time.Duration
	P95Latency    time.Duration
	P99Latency    time.Duration
	MaxLatency    time.Duration
	TotalLogs     int
	TotalCU       int // estimated provider compute units
	Errors        int
	Measurements  []BlockMeasurement
}

type Config struct {
	BlockCount    int
	Concurrency   int
	RangeChunk    int
	CU            CUCosts
	IncludeRanged bool
	BlockNumbers  []uint64
}
