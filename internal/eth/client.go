package eth

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/core/types"

	"github.com/ethereum/go-ethereum/ethclient"
)

type Client struct {
	eth *ethclient.Client
}

func NewClient(ctx context.Context, wsURL string) (*Client, error) {
	ethClient, err := ethclient.DialContext(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}
	return &Client{eth: ethClient}, nil
}

func (c *Client) Close() {
	c.eth.Close()
}

func (c *Client) SubscribeNewBlocks(ctx context.Context) (chan *types.Header, error) {
	headers := make(chan *types.Header)
	sub, err := c.eth.SubscribeNewHead(ctx, headers)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()

	return headers, nil
}
