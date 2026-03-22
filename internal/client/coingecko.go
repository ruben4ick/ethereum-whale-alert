package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type CoinGecko struct {
	client   *http.Client
	cacheTTL time.Duration
	limiter  *rate.Limiter

	mu    sync.RWMutex
	cache map[string]cacheEntry

	// Tokens that need price refresh.
	pendingMu sync.Mutex
	pending   map[string]struct{}
}

type cacheEntry struct {
	price     float64
	fetchedAt time.Time
}

func NewCoinGecko(cacheTTL time.Duration) *CoinGecko {
	return &CoinGecko{
		client:   &http.Client{Timeout: 10 * time.Second},
		cacheTTL: cacheTTL,
		limiter:  rate.NewLimiter(rate.Every(6*time.Second), 1),
		cache:    make(map[string]cacheEntry),
		pending:  make(map[string]struct{}),
	}
}

func (cg *CoinGecko) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cg.refresh(ctx)
		}
	}
}

// PriceInETH returns cached price instantly. Never blocks on network.
// If price is unknown, schedules a background fetch and returns false.
func (cg *CoinGecko) PriceInETH(_ context.Context, tokenAddress string) (float64, bool) {
	addr := strings.ToLower(tokenAddress)

	cg.mu.RLock()
	entry, ok := cg.cache[addr]
	cg.mu.RUnlock()

	if ok {
		return entry.price, true
	}

	// No cached price — schedule background fetch.
	cg.scheduleFetch(addr)
	return 0, false
}

func (cg *CoinGecko) scheduleFetch(addr string) {
	cg.pendingMu.Lock()
	cg.pending[addr] = struct{}{}
	cg.pendingMu.Unlock()
}

// refresh collects pending + stale tokens into a single batch request.
func (cg *CoinGecko) refresh(ctx context.Context) {
	// Collect pending tokens.
	cg.pendingMu.Lock()
	tokens := make([]string, 0, len(cg.pending))
	for addr := range cg.pending {
		tokens = append(tokens, addr)
	}
	cg.pending = make(map[string]struct{})
	cg.pendingMu.Unlock()

	// Collect stale tokens.
	seen := make(map[string]struct{}, len(tokens))
	for _, t := range tokens {
		seen[t] = struct{}{}
	}

	cg.mu.RLock()
	for addr, entry := range cg.cache {
		if time.Since(entry.fetchedAt) > cg.cacheTTL {
			if _, dup := seen[addr]; !dup {
				tokens = append(tokens, addr)
			}
		}
	}
	cg.mu.RUnlock()

	if len(tokens) > 0 {
		cg.fetchBatch(ctx, tokens)
	}
}

func (cg *CoinGecko) fetchBatch(ctx context.Context, tokens []string) {
	const batchSize = 50
	for i := 0; i < len(tokens); i += batchSize {
		end := i + batchSize
		if end > len(tokens) {
			end = len(tokens)
		}
		batch := tokens[i:end]

		if err := cg.limiter.Wait(ctx); err != nil {
			return
		}

		prices, err := cg.fetchMultiple(ctx, batch)
		if err != nil {
			slog.Warn("coingecko batch fetch failed", "tokens", len(batch), "error", err)
			continue
		}

		cg.mu.Lock()
		now := time.Now()
		for addr, price := range prices {
			cg.cache[addr] = cacheEntry{price: price, fetchedAt: now}
		}
		cg.mu.Unlock()

		slog.Debug("prices updated", "count", len(prices))
	}
}

func (cg *CoinGecko) fetchMultiple(ctx context.Context, addrs []string) (map[string]float64, error) {
	joined := strings.Join(addrs, ",")

	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/token_price/ethereum?contract_addresses=%s&vs_currencies=eth",
		joined,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := cg.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		cg.limiter.SetLimit(rate.Every(12 * time.Second))
		return nil, fmt.Errorf("coingecko rate limited (429)")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("coingecko returned status %d", resp.StatusCode)
	}

	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	prices := make(map[string]float64, len(result))
	for addr, data := range result {
		if ethPrice, ok := data["eth"]; ok {
			prices[addr] = ethPrice
		}
	}

	return prices, nil
}
