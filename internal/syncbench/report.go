package syncbench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"time"
)

type MethodSummary struct {
	Method       string `json:"method"`
	Label        string `json:"label"`
	PollInterval string `json:"poll_interval,omitempty"`
	BlocksSeen   int    `json:"blocks_seen"`
	RPCCalls     int    `json:"rpc_calls"`
	BytesIn      int64  `json:"bytes_in"`
	BytesOut     int64  `json:"bytes_out"`
	TotalCU      int    `json:"total_cu"`
	// Latency relative to block.timestamp (header-declared seal time). This is
	// the absolute end-to-end "block sealed → delivered to app" delay and is
	// the headline metric for real-time monitoring.
	AvgLatencyMs float64 `json:"avg_latency_ms"`
	P50LatencyMs float64 `json:"p50_latency_ms"`
	P95LatencyMs float64 `json:"p95_latency_ms"`
	P99LatencyMs float64 `json:"p99_latency_ms"`
	MaxLatencyMs float64 `json:"max_latency_ms"`
	// Latency relative to the fastest method that observed the same block.
	// For WS this is usually 0; for polling it shows the penalty of pull vs push.
	RelAvgMs float64 `json:"rel_avg_ms"`
	RelP50Ms float64 `json:"rel_p50_ms"`
	RelP95Ms float64 `json:"rel_p95_ms"`
	RelP99Ms float64 `json:"rel_p99_ms"`
	RelMaxMs float64 `json:"rel_max_ms"`
}

type Summary struct {
	StartedAt   time.Time       `json:"started_at"`
	FinishedAt  time.Time       `json:"finished_at"`
	DurationSec float64         `json:"duration_sec"`
	CU          CUCosts         `json:"cu_costs"`
	Methods     []MethodSummary `json:"methods"`
}

// BuildSummary aggregates per-method stats. Block timestamps and the
// "fastest method per block" baseline are derived from the union of all runs:
// we treat the WS run as authoritative for header timestamps, and the earliest
// ObservedAt across runs as the baseline for relative latency.
func BuildSummary(out RunOutput, cu CUCosts) Summary {
	s := Summary{
		StartedAt:   out.StartedAt,
		FinishedAt:  out.FinishedAt,
		DurationSec: out.FinishedAt.Sub(out.StartedAt).Seconds(),
		CU:          cu,
	}

	// 1. Build blockNum → timestamp map from any run that carries header
	// timestamps (WS). Polling events have zero BlockTimestamp.
	blockTs := map[uint64]time.Time{}
	for _, r := range out.Runs {
		events, _ := r.snapshot()
		for _, e := range events {
			if !e.BlockTimestamp.IsZero() {
				if existing, ok := blockTs[e.BlockNumber]; !ok || e.BlockTimestamp.Before(existing) {
					blockTs[e.BlockNumber] = e.BlockTimestamp
				}
			}
		}
	}

	// 2. Build blockNum → earliest ObservedAt across all runs (the baseline for
	// relative latency).
	earliest := map[uint64]time.Time{}
	for _, r := range out.Runs {
		events, _ := r.snapshot()
		for _, e := range events {
			if existing, ok := earliest[e.BlockNumber]; !ok || e.ObservedAt.Before(existing) {
				earliest[e.BlockNumber] = e.ObservedAt
			}
		}
	}

	// 3. Per-run summaries.
	for _, r := range out.Runs {
		events, stats := r.snapshot()
		ms := MethodSummary{
			Method:     string(r.Method),
			Label:      r.Label(),
			BlocksSeen: countDistinctBlocks(events),
			RPCCalls:   stats.Calls,
			BytesIn:    stats.BytesIn,
			BytesOut:   stats.BytesOut,
		}
		if r.PollInterval > 0 {
			ms.PollInterval = r.PollInterval.String()
		}
		ms.TotalCU = computeCU(r.Method, stats.Calls, cu)

		var absLat, relLat []time.Duration
		for _, e := range events {
			if ts, ok := blockTs[e.BlockNumber]; ok {
				d := e.ObservedAt.Sub(ts)
				if d < 0 {
					// The header timestamp is set by the proposer and may be
					// slightly in the future relative to wall-clock, especially
					// on testnets — clamp to 0 so the percentiles aren't
					// distorted by tiny negatives.
					d = 0
				}
				absLat = append(absLat, d)
			}
			if base, ok := earliest[e.BlockNumber]; ok {
				relLat = append(relLat, e.ObservedAt.Sub(base))
			}
		}
		fillLatencies(&ms, absLat, relLat)
		s.Methods = append(s.Methods, ms)
	}
	return s
}

func computeCU(m Method, calls int, cu CUCosts) int {
	switch m {
	case MethodWebSocket:
		return calls * cu.SubscribeNewHeadsPerMsg
	case MethodPolling:
		return calls * cu.BlockNumber
	}
	return 0
}

func countDistinctBlocks(events []BlockEvent) int {
	set := make(map[uint64]struct{}, len(events))
	for _, e := range events {
		set[e.BlockNumber] = struct{}{}
	}
	return len(set)
}

func fillLatencies(ms *MethodSummary, abs, rel []time.Duration) {
	if len(abs) > 0 {
		sort.Slice(abs, func(i, j int) bool { return abs[i] < abs[j] })
		var sum time.Duration
		for _, d := range abs {
			sum += d
		}
		ms.AvgLatencyMs = msFloat(sum / time.Duration(len(abs)))
		ms.P50LatencyMs = msFloat(abs[len(abs)*50/100])
		ms.P95LatencyMs = msFloat(abs[min(len(abs)-1, len(abs)*95/100)])
		ms.P99LatencyMs = msFloat(abs[min(len(abs)-1, len(abs)*99/100)])
		ms.MaxLatencyMs = msFloat(abs[len(abs)-1])
	}
	if len(rel) > 0 {
		sort.Slice(rel, func(i, j int) bool { return rel[i] < rel[j] })
		var sum time.Duration
		for _, d := range rel {
			sum += d
		}
		ms.RelAvgMs = msFloat(sum / time.Duration(len(rel)))
		ms.RelP50Ms = msFloat(rel[len(rel)*50/100])
		ms.RelP95Ms = msFloat(rel[min(len(rel)-1, len(rel)*95/100)])
		ms.RelP99Ms = msFloat(rel[min(len(rel)-1, len(rel)*99/100)])
		ms.RelMaxMs = msFloat(rel[len(rel)-1])
	}
}

func msFloat(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

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

// WriteCSV writes one row per (method, block_event) so per-block latency can
// be plotted post-hoc.
func WriteCSV(path string, out RunOutput) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	// Resolve block timestamps once across all runs.
	blockTs := map[uint64]time.Time{}
	for _, r := range out.Runs {
		events, _ := r.snapshot()
		for _, e := range events {
			if !e.BlockTimestamp.IsZero() {
				if existing, ok := blockTs[e.BlockNumber]; !ok || e.BlockTimestamp.Before(existing) {
					blockTs[e.BlockNumber] = e.BlockTimestamp
				}
			}
		}
	}

	header := []string{
		"label", "method", "poll_interval_ms",
		"block_number", "block_timestamp_unix",
		"observed_at_unix_ms", "abs_latency_ms",
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range out.Runs {
		events, _ := r.snapshot()
		intervalMs := ""
		if r.PollInterval > 0 {
			intervalMs = strconv.FormatInt(r.PollInterval.Milliseconds(), 10)
		}
		for _, e := range events {
			tsStr := ""
			absMs := ""
			if ts, ok := blockTs[e.BlockNumber]; ok {
				tsStr = strconv.FormatInt(ts.Unix(), 10)
				d := e.ObservedAt.Sub(ts)
				if d < 0 {
					d = 0
				}
				absMs = strconv.FormatFloat(msFloat(d), 'f', 3, 64)
			}
			row := []string{
				r.Label(),
				string(r.Method),
				intervalMs,
				strconv.FormatUint(e.BlockNumber, 10),
				tsStr,
				strconv.FormatInt(e.ObservedAt.UnixMilli(), 10),
				absMs,
			}
			if err := w.Write(row); err != nil {
				return err
			}
		}
	}
	return nil
}

func PrintComparison(s Summary) {
	fmt.Println()
	fmt.Println("=== Block Discovery Methods Comparison (push vs poll) ===")
	fmt.Printf("Window: %s → %s (%.1fs)\n",
		s.StartedAt.Format(time.RFC3339), s.FinishedAt.Format(time.RFC3339), s.DurationSec)
	fmt.Printf("CU costs: blockNumber=%d, subscribeNewHeads/msg=%d\n",
		s.CU.BlockNumber, s.CU.SubscribeNewHeadsPerMsg)
	fmt.Println()

	fmt.Printf("%-32s %8s %8s %10s %10s %10s %10s %10s %10s %10s\n",
		"Method", "Blocks", "Calls", "BytesIn", "TotalCU",
		"AbsAvg", "AbsP50", "AbsP95", "RelP95", "Max")
	fmt.Println("--------------------------------------------------------------------------------------------------------------------------")
	for _, m := range s.Methods {
		fmt.Printf("%-32s %8d %8d %10s %10d %10.1f %10.1f %10.1f %10.1f %10.1f\n",
			m.Label,
			m.BlocksSeen,
			m.RPCCalls,
			fmtBytes(m.BytesIn),
			m.TotalCU,
			m.AvgLatencyMs,
			m.P50LatencyMs,
			m.P95LatencyMs,
			m.RelP95Ms,
			m.MaxLatencyMs,
		)
	}
	fmt.Println()
	fmt.Println("Latency notation: Abs* = ms from block.timestamp to delivery; Rel* = ms behind fastest method per block.")
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
