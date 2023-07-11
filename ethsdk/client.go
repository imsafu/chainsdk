package ethsdk

import "github.com/ethereum/go-ethereum/ethclient"

func MustDial(rawurl string) *ethclient.Client {
	client, err := ethclient.Dial(rawurl)
	if err != nil {
		panic(err)
	}
	return client
}
