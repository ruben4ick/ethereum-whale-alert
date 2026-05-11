package logbench

import "time"

type Method string

const (
	MethodGetLogsPerBlock        Method = "eth_getLogs (per block)"
	MethodGetLogsRanged          Method = "eth_getLogs (ranged)"
	MethodGetBlockReceipts       Method = "eth_getBlockReceipts"
	MethodGetTransactionReceipts Method = "eth_getTransactionReceipt (per tx)"
)

type BlockMeasurement struct {
	Method      Method
	BlockNumber uint64
	RPCCalls    int
	BytesIn     int64
	BytesOut    int64
	Latency     time.Duration
	LogCount    int
	TxCount     int
	Error       string
}

type MethodResult struct {
	Method        Method
	Blocks        int
	TotalRPCCalls int
	TotalBytesIn  int64
	TotalBytesOut int64
	TotalWall     time.Duration
	TotalCallTime time.Duration
	AvgLatency    time.Duration
	P50Latency    time.Duration
	P95Latency    time.Duration
	P99Latency    time.Duration
	MaxLatency    time.Duration
	TotalLogs     int
	TotalCU       int
	Errors        int
	Measurements  []BlockMeasurement
}

type Config struct {
	BlockCount  int
	Concurrency int
	RangeChunk  int
	CU          CUCosts
}
