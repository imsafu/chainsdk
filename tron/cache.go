package tron

import (
	"sync"

	"github.com/fbsobreira/gotron-sdk/pkg/client"
)

func NewTokenCache(client *client.GrpcClient) *TokenCache {
	return &TokenCache{
		m:      sync.Map{},
		client: client,
	}
}

type TokenCache struct {
	m sync.Map

	client *client.GrpcClient
}

type TokenInfo struct {
	Symbol   string
	Decimals int32
}

func (c *TokenCache) GetTokenInfo(token string) (TokenInfo, error) {
	v, ok := c.m.Load(token)

	if ok {
		return v.(TokenInfo), nil
	}

	symbol, err := c.client.TRC20GetSymbol(token)
	if err != nil {
		return TokenInfo{}, err
	}

	decimals, err := c.client.TRC20GetDecimals(token)
	if err != nil {
		return TokenInfo{}, err
	}

	info := TokenInfo{Symbol: symbol, Decimals: int32(decimals.Int64())}
	c.m.Store(token, info)
	return info, nil
}
