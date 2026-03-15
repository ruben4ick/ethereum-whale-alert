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
	}
}

func (cg *CoinGecko) PriceInETH(ctx context.Context, tokenAddress string) (float64, bool) {
	addr := strings.ToLower(tokenAddress)

	cg.mu.RLock()
	entry, ok := cg.cache[addr]
	cg.mu.RUnlock()

	if ok && time.Since(entry.fetchedAt) < cg.cacheTTL {
		return entry.price, true
	}

	p, err := cg.fetch(ctx, addr)
	if err != nil {
		slog.Warn("coingecko price fetch failed", "token", addr, "error", err)
		// Return stale cache if available.
		if ok {
			return entry.price, true
		}
		return 0, false
	}

	cg.mu.Lock()
	cg.cache[addr] = cacheEntry{price: p, fetchedAt: time.Now()}
	cg.mu.Unlock()

	return p, true
}

func (cg *CoinGecko) fetch(ctx context.Context, addr string) (float64, error) {
	if err := cg.limiter.Wait(ctx); err != nil {
		return 0, fmt.Errorf("rate limiter: %w", err)
	}

	url := fmt.Sprintf(
		"https://api.coingecko.com/api/v3/simple/token_price/ethereum?contract_addresses=%s&vs_currencies=eth",
		addr,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := cg.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		cg.limiter.SetLimit(rate.Every(12 * time.Second))
		return 0, fmt.Errorf("coingecko returned status %d", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("coingecko returned status %d", resp.StatusCode)
	}

	var result map[string]map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode response: %w", err)
	}

	tokenData, ok := result[addr]
	if !ok {
		return 0, fmt.Errorf("token %s not found in response", addr)
	}

	ethPrice, ok := tokenData["eth"]
	if !ok {
		return 0, fmt.Errorf("eth price not found for token %s", addr)
	}

	return ethPrice, nil
}
