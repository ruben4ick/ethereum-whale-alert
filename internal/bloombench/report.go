package bloombench

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

type TopicStats struct {
	TP             int     `json:"tp"`
	FP             int     `json:"fp"`
	TN             int     `json:"tn"`
	FN             int     `json:"fn"`
	FPR            float64 `json:"fpr"`
	BloomMatchRate float64 `json:"bloom_match_rate"`
	Rarity         string  `json:"rarity"`
	Signature      string  `json:"signature"`
}

type Summary struct {
	BlockCount      int                   `json:"block_count"`
	BlocksAnalyzed  int                   `json:"blocks_analyzed"`
	BlocksSkipped   int                   `json:"blocks_skipped_by_bloom"`
	ReceiptCallSave float64               `json:"receipt_calls_saved_pct"`
	TotalLogs       int                   `json:"total_logs_observed"`
	PerTopic        map[string]TopicStats `json:"per_topic"`
}

func WriteCSV(path string, results []BlockResult, topics []Topic) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{"block_number", "log_count", "skipped_by_bloom"}
	for _, t := range topics {
		header = append(header, t.Name+"_bloom", t.Name+"_actual")
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, r := range results {
		if r.BlockNumber == 0 {
			continue
		}
		row := []string{
			strconv.FormatUint(r.BlockNumber, 10),
			strconv.Itoa(r.LogCount),
			strconv.FormatBool(r.Skipped),
		}
		for _, t := range topics {
			o := r.PerTopic[t.Name]
			row = append(row, strconv.FormatBool(o.BloomMatch), strconv.FormatBool(o.Actual))
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return nil
}

func BuildSummary(results []BlockResult, topics []Topic) Summary {
	stats := Aggregate(results, topics)

	analyzed, skipped, totalLogs := 0, 0, 0
	for _, r := range results {
		if r.BlockNumber == 0 {
			continue
		}
		analyzed++
		if r.Skipped {
			skipped++
		}
		totalLogs += r.LogCount
	}

	saved := 0.0
	if analyzed > 0 {
		saved = float64(skipped) / float64(analyzed) * 100.0
	}

	perTopic := make(map[string]TopicStats, len(stats))
	rarityByName := make(map[string]string, len(topics))
	sigByName := make(map[string]string, len(topics))
	for _, t := range topics {
		rarityByName[t.Name] = t.Rarity
		sigByName[t.Name] = t.Signature
	}
	for _, s := range stats {
		matchRate := 0.0
		if analyzed > 0 {
			matchRate = float64(s.TP+s.FP) / float64(analyzed) * 100.0
		}
		perTopic[s.TopicName] = TopicStats{
			TP:             s.TP,
			FP:             s.FP,
			TN:             s.TN,
			FN:             s.FN,
			FPR:            s.FPR(),
			BloomMatchRate: matchRate,
			Rarity:         rarityByName[s.TopicName],
			Signature:      sigByName[s.TopicName],
		}
	}

	return Summary{
		BlockCount:      len(results),
		BlocksAnalyzed:  analyzed,
		BlocksSkipped:   skipped,
		ReceiptCallSave: saved,
		TotalLogs:       totalLogs,
		PerTopic:        perTopic,
	}
}

func WriteJSON(path string, summary Summary) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(summary)
}

func PrintSummary(summary Summary) {
	fmt.Println()
	fmt.Println("=== Bloom Filter Analysis ===")
	fmt.Printf("Blocks analyzed:           %d\n", summary.BlocksAnalyzed)
	fmt.Printf("Blocks skipped by bloom:   %d (%.2f%% receipt-call savings)\n",
		summary.BlocksSkipped, summary.ReceiptCallSave)
	fmt.Printf("Total logs observed:       %d\n", summary.TotalLogs)
	fmt.Println()
	fmt.Printf("%-22s %-18s %-7s %-7s %-7s %-7s %-9s %-9s\n",
		"Topic", "Rarity", "TP", "FP", "TN", "FN", "FPR", "Match%")
	fmt.Println("------------------------------------------------------------------------------------------------")
	for name, s := range summary.PerTopic {
		fmt.Printf("%-22s %-18s %-7d %-7d %-7d %-7d %-9.4f %-9.2f\n",
			name, s.Rarity, s.TP, s.FP, s.TN, s.FN, s.FPR, s.BloomMatchRate)
	}
}
