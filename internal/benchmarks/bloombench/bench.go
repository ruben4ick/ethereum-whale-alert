package bloombench

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type Client interface {
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	BlockReceipts(ctx context.Context, number *big.Int) ([]*types.Receipt, error)
	LatestBlockNumber(ctx context.Context) (uint64, error)
}

type Outcome struct {
	BloomMatch bool
	Actual     bool
}

type BlockResult struct {
	BlockNumber uint64
	LogCount    int
	Skipped     bool // bloombench said no for every topic — receipts not fetched
	PerTopic    map[string]Outcome
}

type Stats struct {
	TopicName string
	TP        int
	FP        int
	TN        int
	FN        int
}

func (s Stats) FPR() float64 {
	denom := s.FP + s.TN
	if denom == 0 {
		return 0
	}
	return float64(s.FP) / float64(denom)
}

type Config struct {
	BlockCount  int
	Concurrency int
	Topics      []Topic
}

type Runner struct {
	client Client
	cfg    Config
}

func New(client Client, cfg Config) *Runner {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 4
	}
	return &Runner{client: client, cfg: cfg}
}

func (r *Runner) Run(ctx context.Context) ([]BlockResult, error) {
	latest, err := r.client.LatestBlockNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("get latest block: %w", err)
	}

	if uint64(r.cfg.BlockCount) > latest {
		return nil, fmt.Errorf("block_count=%d exceeds chain head=%d", r.cfg.BlockCount, latest)
	}
	startBlock := latest - uint64(r.cfg.BlockCount) + 1

	results := make([]BlockResult, r.cfg.BlockCount)
	sem := make(chan struct{}, r.cfg.Concurrency)
	var wg sync.WaitGroup
	var failed int64
	var mu sync.Mutex

	for i := 0; i < r.cfg.BlockCount; i++ {
		blockNum := startBlock + uint64(i)
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, n uint64) {
			defer wg.Done()
			defer func() { <-sem }()

			res, err := r.processBlock(ctx, n)
			if err != nil {
				slog.Warn("block processing failed", "block", n, "error", err)
				mu.Lock()
				failed++
				mu.Unlock()
				return
			}
			results[idx] = res

			if (idx+1)%100 == 0 {
				slog.Info("progress", "processed", idx+1, "total", r.cfg.BlockCount)
			}
		}(i, blockNum)
	}
	wg.Wait()

	if failed > 0 {
		slog.Warn("some blocks failed and were skipped", "failed", failed)
	}
	return results, nil
}

func (r *Runner) processBlock(ctx context.Context, num uint64) (BlockResult, error) {
	header, err := r.client.HeaderByNumber(ctx, new(big.Int).SetUint64(num))
	if err != nil {
		return BlockResult{}, fmt.Errorf("header: %w", err)
	}

	res := BlockResult{
		BlockNumber: num,
		PerTopic:    make(map[string]Outcome, len(r.cfg.Topics)),
	}

	needReceipts := false
	for _, t := range r.cfg.Topics {
		match := types.BloomLookup(header.Bloom, t.Hash)
		res.PerTopic[t.Name] = Outcome{BloomMatch: match}
		if match {
			needReceipts = true
		}
	}

	if !needReceipts {
		res.Skipped = true
		return res, nil
	}

	receipts, err := r.client.BlockReceipts(ctx, new(big.Int).SetUint64(num))
	if err != nil {
		return BlockResult{}, fmt.Errorf("receipts: %w", err)
	}

	actualByTopic := make(map[common.Hash]bool, len(r.cfg.Topics))
	for _, rcpt := range receipts {
		for _, lg := range rcpt.Logs {
			res.LogCount++
			if len(lg.Topics) > 0 {
				actualByTopic[lg.Topics[0]] = true
			}
		}
	}

	for _, t := range r.cfg.Topics {
		o := res.PerTopic[t.Name]
		o.Actual = actualByTopic[t.Hash]
		res.PerTopic[t.Name] = o
	}

	return res, nil
}

func Aggregate(results []BlockResult, topics []Topic) []Stats {
	stats := make([]Stats, 0, len(topics))
	for _, t := range topics {
		s := Stats{TopicName: t.Name}
		for _, r := range results {
			if r.BlockNumber == 0 {
				continue
			}
			o := r.PerTopic[t.Name]
			switch {
			case o.BloomMatch && o.Actual:
				s.TP++
			case o.BloomMatch && !o.Actual:
				s.FP++
			case !o.BloomMatch && !o.Actual:
				s.TN++
			case !o.BloomMatch && o.Actual:
				s.FN++ // must always be 0 — bloombench never lies negatively
			}
		}
		stats = append(stats, s)
	}
	return stats
}
