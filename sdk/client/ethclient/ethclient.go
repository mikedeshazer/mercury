package ethclient

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	coretypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/renproject/mercury/types"
	"github.com/renproject/mercury/types/ethtypes"
)

// EthClient is a client which is used to talking with certain bitcoin network. It can interacting with the blockchain
// through Mercury server.
type EthClient struct {
	url    string
	client *ethclient.Client
}

// NewEthClient returns a new EthClient of given ethereum network.
func NewEthClient(network ethtypes.EthNetwork) (*EthClient, error) {
	var url string
	switch network {
	case ethtypes.EthMainnet:
		url = "http://localhost:5000/eth"
	case ethtypes.EthKovan:
		url = "http://localhost:5000/eth-kovan"
	default:
		return &EthClient{}, types.ErrUnknownNetwork
	}
	return NewCustomEthClient(url)
}

// NewCustomEthClient returns an EthClient for a specific RPC url
func NewCustomEthClient(url string) (*EthClient, error) {
	client, err := ethclient.Dial(url)
	if err != nil {
		return &EthClient{}, err
	}
	return &EthClient{
		url:    url,
		client: client,
	}, nil
}

// Balance returns the balance of the given ethereum address.
func (client *EthClient) Balance(ctx context.Context, address ethtypes.EthAddr) (ethtypes.Amount, error) {
	value, err := client.client.BalanceAt(ctx, common.Address(address), nil)
	if err != nil {
		return ethtypes.Amount{}, err
	}
	return ethtypes.WeiFromBig(value), nil
}

// BlockNumber returns the current highest block number.
func (client *EthClient) BlockNumber(ctx context.Context) (*big.Int, error) {
	value, err := client.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return nil, err
	}
	return value.Number, nil
}

// PublishSTX publishes a signed transaction
func (client *EthClient) PublishSTX(ctx context.Context, stx ethtypes.EthSignedTx) error {
	return client.client.SendTransaction(ctx, (*coretypes.Transaction)(stx))
}
