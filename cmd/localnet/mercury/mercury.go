package main

import (
	"os"

	"github.com/renproject/kv"
	"github.com/renproject/mercury/api"
	"github.com/renproject/mercury/cache"
	"github.com/renproject/mercury/proxy"
	"github.com/renproject/mercury/rpc"
	"github.com/renproject/mercury/types/btctypes"
	"github.com/renproject/mercury/types/ethtypes"
	"github.com/sirupsen/logrus"
)

func main() {
	// Initialise logger.
	logger := logrus.StandardLogger()

	// Initialise stores.
	store := kv.NewMemDB(kv.JSONCodec)

	btcStore := kv.NewTable(store, "btc")
	btcCache := cache.New(btcStore, logger)
	zecStore := kv.NewTable(store, "zec")
	zecCache := cache.New(zecStore, logger)
	bchStore := kv.NewTable(store, "bch")
	bchCache := cache.New(bchStore, logger)
	ethStore := kv.NewTable(store, "eth")
	ethCache := cache.New(ethStore, logger)

	// Initialise Bitcoin API.
	btcNodeClient := rpc.NewClient(os.Getenv("BTC_RPC_URL"), "user", "password")
	btcProxy := proxy.NewProxy(btcNodeClient)
	btcAPI := api.NewApi(btctypes.BtcLocalnet, btcProxy, btcCache, logger)

	// Initialise ZCash API.
	zecNodeClient := rpc.NewClient(os.Getenv("ZEC_RPC_URL"), "user", "password")
	zecProxy := proxy.NewProxy(zecNodeClient)
	zecAPI := api.NewApi(btctypes.ZecLocalnet, zecProxy, zecCache, logger)

	// Initialise BCash API.
	bchNodeClient := rpc.NewClient(os.Getenv("BCH_RPC_URL"), "user", "password")
	bchProxy := proxy.NewProxy(bchNodeClient)
	bchAPI := api.NewApi(btctypes.BchLocalnet, bchProxy, bchCache, logger)

	ethNodeClient := rpc.NewClient(os.Getenv("ETH_RPC_URL"), "", "")
	ethProxy := proxy.NewProxy(ethNodeClient)
	ethAPI := api.NewApi(ethtypes.EthLocalnet, ethProxy, ethCache, logger)

	// Set-up and start the server.
	server := api.NewServer(logger, "5000", btcAPI, zecAPI, bchAPI, ethAPI)
	server.Run()
}