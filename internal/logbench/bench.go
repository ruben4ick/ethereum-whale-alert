package logbench

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

type Runner struct {
	rpc *RawRPC
	cfg Config
}

func New(rpc *RawRPC, cfg Config) *Runner {
	return &Runner{rpc: rpc, cfg: cfg}
}

// PickBlocks picks a contiguous range of the latest cfg.BlockCount blocks.
func (r *Runner) PickBlocks(ctx context.Context) ([]uint64, error) {
	resp, result, err := r.rpc.Call(ctx, "eth_blockNumber", []any{}, tputBlockNumber)
	if err != nil {
		return nil, fmt.Errorf("blockNumber: %w", err)
	}
	_ = resp
	var hex string
	if err := decodeJSON(result, &hex); err != nil {
		return nil, fmt.Errorf("decode blockNumber: %w", err)
	}
	latest, err := parseHexUint(hex)
	if err != nil {
		return nil, err
	}
	if uint64(r.cfg.BlockCount) > latest {
		return nil, fmt.Errorf("block_count=%d exceeds chain head=%d", r.cfg.BlockCount, latest)
	}
	start := latest - uint64(r.cfg.BlockCount) + 1
	blocks := make([]uint64, r.cfg.BlockCount)
	for i := range blocks {
		blocks[i] = start + uint64(i)
	}
	return blocks, nil
}

type RunOutput struct {
	Results    []MethodResult
	Mismatches []string // empty if log counts agree across methods, per block
}

func (r *Runner) Run(ctx context.Context, blocks []uint64) RunOutput {
	out := RunOutput{Results: make([]MethodResult, 0, 4)}

	bc := len(blocks)

	slog.Info("running method", "method", MethodGetLogsPerBlock)
	t0 := time.Now()
	perBlock := RunGetLogsPerBlock(ctx, r.rpc, blocks, r.cfg.Concurrency)
	out.Results = append(out.Results, summarize(MethodGetLogsPerBlock, perBlock, time.Since(t0), r.cfg.CU, bc))

	slog.Info("running method", "method", MethodGetLogsRanged, "chunk_blocks", r.cfg.RangeChunk)
	t0 = time.Now()
	chunks := RunGetLogsRanged(ctx, r.rpc, blocks, r.cfg.RangeChunk)
	rangedMs := chunkToMeasurements(chunks)
	out.Results = append(out.Results, summarize(MethodGetLogsRanged, rangedMs, time.Since(t0), r.cfg.CU, bc))
	rangedByBlk := mergeChunks(chunks)

	slog.Info("running method", "method", MethodGetBlockReceipts)
	t0 = time.Now()
	blkRcpts := RunGetBlockReceipts(ctx, r.rpc, blocks, r.cfg.Concurrency)
	out.Results = append(out.Results, summarize(MethodGetBlockReceipts, blkRcpts, time.Since(t0), r.cfg.CU, bc))

	slog.Info("running method", "method", MethodGetTransactionReceipts)
	t0 = time.Now()
	perTx := RunGetTransactionReceiptsPerTx(ctx, r.rpc, blocks, r.cfg.Concurrency)
	out.Results = append(out.Results, summarize(MethodGetTransactionReceipts, perTx, time.Since(t0), r.cfg.CU, bc))

	out.Mismatches = CrossCheckLogs(blocks, perBlock, rangedByBlk, blkRcpts, perTx)
	return out
}

func mergeChunks(chunks []ChunkMeasurement) map[uint64]int {
	out := make(map[uint64]int)
	for _, c := range chunks {
		for b, n := range c.LogsByBlk {
			out[b] += n
		}
	}
	return out
}

func chunkToMeasurements(chunks []ChunkMeasurement) []BlockMeasurement {
	out := make([]BlockMeasurement, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Chunk)
	}
	return out
}

func summarize(method Method, ms []BlockMeasurement, wall time.Duration, cu CUCosts, blockCount int) MethodResult {
	r := MethodResult{
		Method:       method,
		Blocks:       blockCount,
		TotalWall:    wall,
		Measurements: ms,
	}
	latencies := make([]time.Duration, 0, len(ms))
	for _, m := range ms {
		if m.Error != "" {
			r.Errors++
			continue
		}
		r.TotalRPCCalls += m.RPCCalls
		r.TotalBytesIn += m.BytesIn
		r.TotalBytesOut += m.BytesOut
		r.TotalCallTime += m.Latency
		r.TotalLogs += m.LogCount
		latencies = append(latencies, m.Latency)
	}
	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		r.AvgLatency = r.TotalCallTime / time.Duration(len(latencies))
		r.P50Latency = latencies[len(latencies)*50/100]
		r.P95Latency = latencies[min(len(latencies)-1, len(latencies)*95/100)]
		r.P99Latency = latencies[min(len(latencies)-1, len(latencies)*99/100)]
		r.MaxLatency = latencies[len(latencies)-1]
	}
	r.TotalCU = computeCU(method, ms, cu)
	return r
}

func computeCU(method Method, ms []BlockMeasurement, cu CUCosts) int {
	total := 0
	switch method {
	case MethodGetLogsPerBlock, MethodGetLogsRanged:
		// One getLogs call per measurement.
		total = len(ms) * cu.GetLogs
	case MethodGetBlockReceipts:
		total = len(ms) * cu.GetBlockReceipts
	case MethodGetTransactionReceipts:
		// Each block costs 1× getBlockByNumber + N× getTransactionReceipt.
		for _, m := range ms {
			total += cu.GetBlockByNumber
			// RPCCalls = 1 (block) + N (receipts), so N = RPCCalls - 1.
			n := m.RPCCalls - 1
			if n < 0 {
				n = 0
			}
			total += n * cu.GetTransactionReceipt
		}
	}
	return total
}

// CrossCheckLogs verifies that all per-block methods agree on the log count
// for every block. Returns a list of human-readable mismatches (empty if all
// methods agree). Ranged is normalized via the LogsByBlk map.
func CrossCheckLogs(blocks []uint64, perBlock []BlockMeasurement, rangedBlk map[uint64]int, blkRcpts, perTx []BlockMeasurement) []string {
	pb := indexBy(perBlock)
	br := indexBy(blkRcpts)
	pt := indexBy(perTx)
	var mismatches []string
	for _, n := range blocks {
		v1 := pb[n]
		v2 := rangedBlk[n]
		v3 := br[n]
		v4 := pt[n]
		if v1 == v2 && v2 == v3 && v3 == v4 {
			continue
		}
		mismatches = append(mismatches,
			fmt.Sprintf("block %d: getLogs/block=%d, getLogs/ranged=%d, blockReceipts=%d, txReceipts=%d",
				n, v1, v2, v3, v4))
	}
	return mismatches
}

func indexBy(ms []BlockMeasurement) map[uint64]int {
	out := make(map[uint64]int, len(ms))
	for _, m := range ms {
		out[m.BlockNumber] = m.LogCount
	}
	return out
}

// decodeJSON is a tiny helper to keep call sites readable.
func decodeJSON(raw []byte, dst any) error {
	return json.Unmarshal(raw, dst)
}
