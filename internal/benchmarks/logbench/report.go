package logbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"
)

type MethodSummary struct {
	Method        string  `json:"method"`
	Blocks        int     `json:"blocks"`
	TotalRPCCalls int     `json:"total_rpc_calls"`
	TotalBytesIn  int64   `json:"total_bytes_in"`
	TotalBytesOut int64   `json:"total_bytes_out"`
	TotalCU       int     `json:"total_cu"`
	TotalLogs     int     `json:"total_logs"`
	Errors        int     `json:"errors"`
	WallMs        int64   `json:"wall_ms"`
	AvgLatencyMs  float64 `json:"avg_latency_ms"`
	P50LatencyMs  float64 `json:"p50_latency_ms"`
	P95LatencyMs  float64 `json:"p95_latency_ms"`
	P99LatencyMs  float64 `json:"p99_latency_ms"`
	MaxLatencyMs  float64 `json:"max_latency_ms"`
	BytesPerBlock int64   `json:"bytes_per_block_avg"`
	CUPerLog      float64 `json:"cu_per_log"`
}

type Summary struct {
	BlockCount int             `json:"block_count"`
	StartBlock uint64          `json:"start_block"`
	EndBlock   uint64          `json:"end_block"`
	CU         CUCosts         `json:"cu_costs"`
	Methods    []MethodSummary `json:"methods"`
}

func BuildSummary(blocks []uint64, results []MethodResult, cu CUCosts) Summary {
	s := Summary{
		BlockCount: len(blocks),
		CU:         cu,
	}
	if len(blocks) > 0 {
		s.StartBlock = blocks[0]
		s.EndBlock = blocks[len(blocks)-1]
	}
	for _, r := range results {
		ms := MethodSummary{
			Method:        string(r.Method),
			Blocks:        r.Blocks,
			TotalRPCCalls: r.TotalRPCCalls,
			TotalBytesIn:  r.TotalBytesIn,
			TotalBytesOut: r.TotalBytesOut,
			TotalCU:       r.TotalCU,
			TotalLogs:     r.TotalLogs,
			Errors:        r.Errors,
			WallMs:        r.TotalWall.Milliseconds(),
			AvgLatencyMs:  msFloat(r.AvgLatency),
			P50LatencyMs:  msFloat(r.P50Latency),
			P95LatencyMs:  msFloat(r.P95Latency),
			P99LatencyMs:  msFloat(r.P99Latency),
			MaxLatencyMs:  msFloat(r.MaxLatency),
		}
		if len(blocks) > 0 {
			ms.BytesPerBlock = r.TotalBytesIn / int64(len(blocks))
		}
		if r.TotalLogs > 0 {
			ms.CUPerLog = float64(r.TotalCU) / float64(r.TotalLogs)
		}
		s.Methods = append(s.Methods, ms)
	}
	return s
}

func msFloat(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

func WriteJSON(path string, s Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

func WriteCSV(path string, results []MethodResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"method", "block_number", "rpc_calls",
		"bytes_in", "bytes_out", "latency_ms",
		"log_count", "tx_count", "error",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range results {
		for _, m := range r.Measurements {
			row := []string{
				string(r.Method),
				strconv.FormatUint(m.BlockNumber, 10),
				strconv.Itoa(m.RPCCalls),
				strconv.FormatInt(m.BytesIn, 10),
				strconv.FormatInt(m.BytesOut, 10),
				strconv.FormatFloat(msFloat(m.Latency), 'f', 3, 64),
				strconv.Itoa(m.LogCount),
				strconv.Itoa(m.TxCount),
				m.Error,
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return nil
}

func PrintComparison(s Summary, mismatches []string) {
	fmt.Println()
	fmt.Println("=== Log Retrieval Methods Comparison ===")
	fmt.Printf("Block range: %d..%d (%d blocks)\n", s.StartBlock, s.EndBlock, s.BlockCount)
	fmt.Printf("CU costs (Alchemy): getLogs=%d, getBlockReceipts=%d, getTxReceipt=%d, getBlockByNumber=%d\n",
		s.CU.GetLogs, s.CU.GetBlockReceipts, s.CU.GetTransactionReceipt, s.CU.GetBlockByNumber)
	fmt.Println()

	fmt.Printf("%-38s %8s %10s %12s %10s %10s %10s %10s %10s %10s\n",
		"Method", "Calls", "BytesIn", "Bytes/Blk", "Wall ms", "Avg ms", "P50 ms", "P95 ms", "Total CU", "CU/Log")
	fmt.Println("-----------------------------------------------------------------------------------------------------------------------------------------------------")
	for _, m := range s.Methods {
		fmt.Printf("%-38s %8d %10s %12s %10d %10.1f %10.1f %10.1f %10d %10.3f\n",
			m.Method,
			m.TotalRPCCalls,
			fmtBytes(m.TotalBytesIn),
			fmtBytes(m.BytesPerBlock),
			m.WallMs,
			m.AvgLatencyMs,
			m.P50LatencyMs,
			m.P95LatencyMs,
			m.TotalCU,
			m.CUPerLog,
		)
	}

	fmt.Println()
	fmt.Println("Log counts (sanity check — should match across methods):")
	for _, m := range s.Methods {
		fmt.Printf("  %-38s logs=%d errors=%d\n", m.Method, m.TotalLogs, m.Errors)
	}
	if len(mismatches) > 0 {
		fmt.Printf("\n%d block(s) had per-block log-count mismatches between methods:\n", len(mismatches))
		shown := mismatches
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for _, m := range shown {
			fmt.Printf("  %s\n", m)
		}
		if len(mismatches) > 10 {
			fmt.Printf("  ... and %d more (see CSV)\n", len(mismatches)-10)
		}
	} else {
		fmt.Println("\nAll methods agreed on per-block log counts.")
	}
}

func fmtBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * 1024
	)
	switch {
	case b >= mb:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
