package logbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// minLog parses only what we need from the JSON-RPC log object.
type minLog struct {
	BlockNumber string `json:"blockNumber"` // hex
}

type minReceipt struct {
	Logs []json.RawMessage `json:"logs"`
}

type minBlock struct {
	Transactions []string `json:"transactions"` // when full=false: array of tx hashes
}

func hexBlock(n uint64) string { return fmt.Sprintf("0x%x", n) }

func parseHexUint(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	return strconv.ParseUint(s, 16, 64)
}

// runWorkers executes fn for each block with the given concurrency. It collects
// per-block measurements in input order. Total wall-clock time is measured
// around the whole loop (so it reflects the chosen concurrency).
func runWorkers(ctx context.Context, blocks []uint64, concurrency int, fn func(context.Context, uint64) BlockMeasurement) ([]BlockMeasurement, time.Duration) {
	results := make([]BlockMeasurement, len(blocks))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	start := time.Now()
	for i, n := range blocks {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, n uint64) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = fn(ctx, n)
		}(i, n)
	}
	wg.Wait()
	return results, time.Since(start)
}

// --- Method 1: eth_getLogs per block ---------------------------------------

func RunGetLogsPerBlock(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	results, _ := runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetLogsPerBlock, BlockNumber: n}
		params := []any{map[string]any{"fromBlock": hexBlock(n), "toBlock": hexBlock(n)}}
		resp, result, err := rpc.Call(ctx, "eth_getLogs", params)
		if err != nil {
			m.Error = err.Error()
			return m
		}
		var logs []json.RawMessage
		_ = json.Unmarshal(result, &logs)
		m.RPCCalls = 1
		m.BytesIn = resp.BytesIn
		m.BytesOut = resp.BytesOut
		m.Latency = resp.Latency
		m.LogCount = len(logs)
		return m
	})
	return results
}

// --- Method 2: eth_getLogs ranged (chunked) --------------------------------

// RunGetLogsRanged groups the input blocks into contiguous chunks of size
// chunkSize and issues one eth_getLogs per chunk. Returns one BlockMeasurement
// per chunk (BlockNumber = first block of chunk). The TotalLogs returned by
// the chunk is split into per-block counts via the blockNumber field on each
// log, so callers can cross-check against per-block methods.
type ChunkMeasurement struct {
	Chunk      BlockMeasurement // aggregated chunk-level RPC stats
	LogsByBlk  map[uint64]int   // log count keyed by block number, derived from response
	StartBlock uint64
	EndBlock   uint64
}

func RunGetLogsRanged(ctx context.Context, rpc *RawRPC, blocks []uint64, chunkSize int) []ChunkMeasurement {
	if chunkSize < 1 {
		chunkSize = 1
	}
	if len(blocks) == 0 {
		return nil
	}
	// blocks must already be sorted ascending and contiguous for the ranged
	// query to be meaningful; the runner guarantees this.
	chunks := make([]ChunkMeasurement, 0, (len(blocks)+chunkSize-1)/chunkSize)
	for start := 0; start < len(blocks); start += chunkSize {
		end := start + chunkSize
		if end > len(blocks) {
			end = len(blocks)
		}
		from := blocks[start]
		to := blocks[end-1]

		m := BlockMeasurement{Method: MethodGetLogsRanged, BlockNumber: from}
		params := []any{map[string]any{"fromBlock": hexBlock(from), "toBlock": hexBlock(to)}}
		resp, result, err := rpc.Call(ctx, "eth_getLogs", params)
		cm := ChunkMeasurement{StartBlock: from, EndBlock: to, LogsByBlk: map[uint64]int{}}
		if err != nil {
			m.Error = err.Error()
			cm.Chunk = m
			chunks = append(chunks, cm)
			continue
		}
		var logs []minLog
		_ = json.Unmarshal(result, &logs)
		for _, lg := range logs {
			bn, err := parseHexUint(lg.BlockNumber)
			if err != nil {
				continue
			}
			cm.LogsByBlk[bn]++
		}
		m.RPCCalls = 1
		m.BytesIn = resp.BytesIn
		m.BytesOut = resp.BytesOut
		m.Latency = resp.Latency
		m.LogCount = len(logs)
		cm.Chunk = m
		chunks = append(chunks, cm)
	}
	return chunks
}

// --- Method 3: eth_getBlockReceipts per block ------------------------------

func RunGetBlockReceipts(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	results, _ := runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetBlockReceipts, BlockNumber: n}
		resp, result, err := rpc.Call(ctx, "eth_getBlockReceipts", []any{hexBlock(n)})
		if err != nil {
			m.Error = err.Error()
			return m
		}
		var receipts []minReceipt
		_ = json.Unmarshal(result, &receipts)
		m.RPCCalls = 1
		m.BytesIn = resp.BytesIn
		m.BytesOut = resp.BytesOut
		m.Latency = resp.Latency
		m.TxCount = len(receipts)
		for _, r := range receipts {
			m.LogCount += len(r.Logs)
		}
		return m
	})
	return results
}

// --- Method 4: eth_getTransactionReceipt per tx ----------------------------
// Requires a preliminary eth_getBlockByNumber(false) per block to learn the
// transaction hashes; we attribute that call's cost to the same block bucket,
// since the workflow as a whole is what costs the user.

func RunGetTransactionReceiptsPerTx(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	results, _ := runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetTransactionReceipts, BlockNumber: n}

		// Step 1: fetch block to learn the tx hashes.
		resp, result, err := rpc.Call(ctx, "eth_getBlockByNumber", []any{hexBlock(n), false})
		if err != nil {
			m.Error = "block: " + err.Error()
			return m
		}
		var blk minBlock
		if err := json.Unmarshal(result, &blk); err != nil {
			m.Error = "decode block: " + err.Error()
			return m
		}
		m.RPCCalls = 1
		m.BytesIn = resp.BytesIn
		m.BytesOut = resp.BytesOut
		m.Latency = resp.Latency
		m.TxCount = len(blk.Transactions)

		// Step 2: one getTransactionReceipt per tx, sequential per block (the
		// outer worker pool already provides parallelism across blocks).
		for _, txh := range blk.Transactions {
			rresp, rresult, err := rpc.Call(ctx, "eth_getTransactionReceipt", []any{txh})
			if err != nil {
				m.Error = "receipt: " + err.Error()
				return m
			}
			var rcpt minReceipt
			if err := json.Unmarshal(rresult, &rcpt); err != nil {
				m.Error = "decode receipt: " + err.Error()
				return m
			}
			m.RPCCalls++
			m.BytesIn += rresp.BytesIn
			m.BytesOut += rresp.BytesOut
			m.Latency += rresp.Latency
			m.LogCount += len(rcpt.Logs)
		}
		return m
	})
	return results
}
