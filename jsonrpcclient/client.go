package jsonrpcclient

import (
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

func NewClient(rpcURL string) *Client {
	c := resty.New()
	c.SetTimeout(30 * time.Second)
	c.SetBaseURL(rpcURL)
	return &Client{c}
}

// used to call any RPC method, usually used for testing (call anvil methods)
type Client struct {
	*resty.Client
}

func (c *Client) Call(method string, params []any, result interface{}) error {
	hres, err := c.R().
		SetResult(&result).
		SetBody(c.jsonRPCBody(method, params)).
		Post("/")
	if err != nil {
		return err
	}

	if hres.IsError() {
		return fmt.Errorf("json call: %d %s", hres.StatusCode(), hres.Status())
	}

	jsonResult, ok := result.(map[string]interface{})
	if ok && jsonResult["error"] != nil {
		return fmt.Errorf("json call: %v", jsonResult["error"])
	}

	return nil
}

type JSONRPCResult[T any] struct {
	ID     string `json:"id"`
	Result T      `json:"result"`
}

func (*Client) jsonRPCBody(method string, params []any) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      "id",
		"method":  method,
		"params":  params,
	}
}
