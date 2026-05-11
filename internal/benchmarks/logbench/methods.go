package logbench

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

const (
	tputBlockNumber      = 10
	tputGetLogs          = 60
	tputGetBlockByNumber = 20
	tputGetBlockReceipts = 500
	tputGetTxReceipt     = 20
)

type minLog struct {
	BlockNumber string `json:"blockNumber"`
}

type minReceipt struct {
	Logs []json.RawMessage `json:"logs"`
}

type minBlock struct {
	Transactions []string `json:"transactions"`
}

func hexBlock(n uint64) string { return fmt.Sprintf("0x%x", n) }

func parseHexUint(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	return strconv.ParseUint(s, 16, 64)
}

func runWorkers(ctx context.Context, blocks []uint64, concurrency int, fn func(context.Context, uint64) BlockMeasurement) []BlockMeasurement {
	results := make([]BlockMeasurement, len(blocks))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
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
	return results
}

func RunGetLogsPerBlock(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	return runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetLogsPerBlock, BlockNumber: n}
		params := []any{map[string]any{"fromBlock": hexBlock(n), "toBlock": hexBlock(n)}}
		resp, result, err := rpc.Call(ctx, "eth_getLogs", params, tputGetLogs)
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
}

type ChunkMeasurement struct {
	Chunk      BlockMeasurement
	LogsByBlk  map[uint64]int
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
	chunks := make([]ChunkMeasurement, 0, (len(blocks)+chunkSize-1)/chunkSize)
	for start := 0; start < len(blocks); start += chunkSize {
		end := start + chunkSize
		if end > len(blocks) {
			end = len(blocks)
		}
		from, to := blocks[start], blocks[end-1]

		m := BlockMeasurement{Method: MethodGetLogsRanged, BlockNumber: from}
		params := []any{map[string]any{"fromBlock": hexBlock(from), "toBlock": hexBlock(to)}}
		resp, result, err := rpc.Call(ctx, "eth_getLogs", params, tputGetLogs)
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

func RunGetBlockReceipts(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	return runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetBlockReceipts, BlockNumber: n}
		resp, result, err := rpc.Call(ctx, "eth_getBlockReceipts", []any{hexBlock(n)}, tputGetBlockReceipts)
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
}

func RunGetTransactionReceiptsPerTx(ctx context.Context, rpc *RawRPC, blocks []uint64, concurrency int) []BlockMeasurement {
	return runWorkers(ctx, blocks, concurrency, func(ctx context.Context, n uint64) BlockMeasurement {
		m := BlockMeasurement{Method: MethodGetTransactionReceipts, BlockNumber: n}

		resp, result, err := rpc.Call(ctx, "eth_getBlockByNumber", []any{hexBlock(n), false}, tputGetBlockByNumber)
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

		for _, txh := range blk.Transactions {
			rresp, rresult, err := rpc.Call(ctx, "eth_getTransactionReceipt", []any{txh}, tputGetTxReceipt)
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
}
