package client

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

type EthereumClient struct {
	client *ethclient.Client
}

func NewEthereumClient(ctx context.Context, wsURL string) (*EthereumClient, error) {
	ethClient, err := ethclient.DialContext(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}
	return &EthereumClient{client: ethClient}, nil
}

func (c *EthereumClient) Close() {
	c.client.Close()
}

func (c *EthereumClient) GetBlockByNumber(ctx context.Context, number *big.Int) (*types.Block, error) {
	return c.client.BlockByNumber(ctx, number)
}

func (c *EthereumClient) SubscribeNewBlocks(ctx context.Context) (chan *types.Header, error) {
	headers := make(chan *types.Header)
	sub, err := c.client.SubscribeNewHead(ctx, headers)
	if err != nil {
		return nil, err
	}

	go func() {
		<-ctx.Done()
		sub.Unsubscribe()
	}()

	return headers, nil
}
