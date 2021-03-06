package btcclient

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/renproject/mercury/rpcclient"
	"github.com/renproject/mercury/rpcclient/btcrpcclient"
	mclient "github.com/renproject/mercury/sdk/client"
	"github.com/renproject/mercury/types"
	"github.com/renproject/mercury/types/btctypes"
	"github.com/sirupsen/logrus"
)

const (
	Dust = btctypes.Amount(600)
)

// Client is a client which is used to talking with certain Bitcoin network. It can interacting with the blockchain
// through Mercury server.
type client struct {
	client     btcrpcclient.Client
	network    btctypes.Network
	config     chaincfg.Params
	url        string
	gasStation BtcGasStation
	logger     logrus.FieldLogger
}

type BtcClient struct {
	Client
}

type ZecClient struct {
	Client
}

type BchClient struct {
	Client
}

func MercuryURL(network btctypes.Network) string {
	switch network.String() {
	case btctypes.BtcMainnet.String(), btctypes.ZecMainnet.String(), btctypes.BchMainnet.String():
		return fmt.Sprintf("%s/%s/mainnet", mclient.MercuryURL, network.Chain().String())
	case btctypes.BtcTestnet.String(), btctypes.ZecTestnet.String(), btctypes.BchTestnet.String():
		return fmt.Sprintf("%s/%s/testnet", mclient.MercuryURL, network.Chain().String())
	case btctypes.BtcLocalnet.String(), btctypes.ZecLocalnet.String(), btctypes.BchLocalnet.String():
		return fmt.Sprintf("http://0.0.0.0:5000/%s/testnet", network.Chain().String())
	default:
		panic(types.ErrUnknownNetwork)
	}
}

// NewClient returns a new Client of given network.
func NewClient(logger logrus.FieldLogger, network btctypes.Network) Client {
	host := MercuryURL(network)
	gasStation := NewBtcGasStation(logger, 30*time.Minute)
	baseClient := &client{
		client:     btcrpcclient.NewRPCClient(host, "", "", 5*time.Second),
		network:    network,
		config:     *network.Params(),
		url:        host,
		gasStation: gasStation,
		logger:     logger,
	}
	switch network.Chain() {
	case types.Bitcoin:
		return &BtcClient{baseClient}
	case types.ZCash:
		return &ZecClient{baseClient}
	case types.BitcoinCash:
		return &BchClient{baseClient}
	default:
		panic(types.ErrUnknownChain)
	}
}

func (c *client) Network() btctypes.Network {
	return c.network
}

// UTXO returns the UTXO for the given transaction hash and index.
func (c *client) UTXO(ctx context.Context, op btctypes.OutPoint) (btctypes.UTXO, error) {
	if len(op.TxHash()) != 64 {
		return nil, NewErrInvalidTxHash(fmt.Errorf(string(op.TxHash())))
	}

	tx, err := c.client.GetRawTransactionVerbose(ctx, op.TxHash())
	if err != nil {
		return nil, NewErrTxHashNotFound(err)
	}

	txOut, err := c.client.GetTxOut(ctx, op.TxHash(), op.Vout())
	if err != nil {
		if err == rpcclient.ErrNullResult {
			return nil, NewErrUTXOSpent(err)
		}
		return nil, fmt.Errorf("cannot get tx output from btc client: %v", err)
	}

	amount, err := btcutil.NewAmount(txOut.Value)
	if err != nil {
		return nil, fmt.Errorf("cannot parse amount received from btc client: %v", err)
	}

	scriptPubKey, err := hex.DecodeString(txOut.ScriptPubKey.Hex)
	if err != nil {
		return nil, fmt.Errorf("cannot decode script pubkey")
	}

	return btctypes.NewUTXO(
		btctypes.NewOutPoint(types.TxHash(tx.TxID), op.Vout()),
		btctypes.Amount(amount),
		scriptPubKey,
		uint64(txOut.Confirmations),
		nil,
	), nil
}

// UTXOsFromAddress returns the UTXOs for a given address. Important: this function will not return any UTXOs for
// addresses that have not been imported into the Bitcoin node.
func (c *client) UTXOsFromAddress(ctx context.Context, address btctypes.Address) (btctypes.UTXOs, error) {
	outputs, err := c.client.ListUnspent(ctx, 0, 999999, []btctypes.Address{address})
	if err != nil {
		return nil, fmt.Errorf("cannot retrieve utxos from btc client: %v", err)
	}

	utxos := make(btctypes.UTXOs, len(outputs))
	for i, output := range outputs {
		amount, err := btcutil.NewAmount(output.Amount)
		if err != nil {
			return nil, fmt.Errorf("cannot parse amount received from btc client: %v", err)
		}

		scriptPubKey, err := hex.DecodeString(output.ScriptPubKey)
		if err != nil {
			return nil, fmt.Errorf("cannot decode script pubkey")
		}

		utxos[i] = btctypes.NewUTXO(
			btctypes.NewOutPoint(types.TxHash(output.TxID), output.Vout),
			btctypes.Amount(amount),
			scriptPubKey,
			uint64(output.Confirmations),
			nil,
		)
	}

	return utxos, nil
}

// Confirmations returns the number of confirmation blocks of the given txHash.
func (c *client) Confirmations(ctx context.Context, txHash types.TxHash) (uint64, error) {
	tx, err := c.client.GetRawTransactionVerbose(ctx, txHash)
	if err != nil {
		return 0, fmt.Errorf("cannot get tx from hash %s: %v", txHash, err)
	}
	return uint64(tx.Confirmations), nil
}

func (c *client) BuildUnsignedTx(utxos btctypes.UTXOs, recipients btctypes.Recipients, refundTo btctypes.Address, gas btctypes.Amount) (btctypes.BtcTx, error) {
	// Pre-condition checks.
	if gas < Dust {
		return nil, fmt.Errorf("pre-condition violation: gas = %v is too low", gas)
	}

	amountFromUTXOs := utxos.Sum()
	if amountFromUTXOs < Dust {
		return nil, fmt.Errorf("pre-condition violation: amount=%v from utxos is less than dust=%v", amountFromUTXOs, Dust)
	}

	// Add an output for each recipient and sum the total amount that is being transferred to recipients.
	amountToRecipients := btctypes.Amount(0)
	for _, recipient := range recipients {
		amountToRecipients += recipient.Amount
	}

	// Check that we are not transferring more to recipients than available in the UTXOs (accounting for gas).
	amountToRefund := amountFromUTXOs - amountToRecipients - gas
	if amountToRefund < 0 {
		return nil, fmt.Errorf("insufficient balance: expected %v, got %v", amountToRecipients+gas, amountFromUTXOs)
	}

	// Add an output to refund the difference between the amount being transferred to recipients and the total amount
	// from the UTXOs, if it is greater than the Dust amount.
	if amountToRefund > Dust {
		recipients = append(recipients, btctypes.NewRecipient(refundTo, amountToRefund))
	}

	// Get the signature hashes we need to sign.
	return btctypes.NewUnsignedTx(c.network, utxos, recipients)
}

// SubmitSignedTx submits the signed transaction and returns the transaction hash in hex.
func (c *client) SubmitSignedTx(ctx context.Context, stx btctypes.BtcTx) (types.TxHash, error) {
	// Pre-condition checks
	if !stx.IsSigned() {
		return "", errors.New("pre-condition violation: cannot submit unsigned transaction")
	}
	if err := c.VerifyTx(stx); err != nil {
		return "", fmt.Errorf("pre-condition violation: transaction failed verification: %v", err)
	}

	txHash, err := c.client.SendRawTransaction(ctx, stx)
	if err != nil {
		return "", fmt.Errorf("cannot send raw transaction using btc client: %v", err)
	}
	return types.TxHash(txHash), nil
}

// EstimateTxSize estimates the tx size depending on number of utxos used and recipients. DEPRICATED use
// btctypes.EstimateTxSize() instead.
func (c *client) EstimateTxSize(numUTXOs, numRecipients int) int {
	return 146*numUTXOs + 33*numRecipients + 10
}

func (c *client) VerifyTx(tx btctypes.BtcTx) error {
	if c.network.Chain() != types.Bitcoin {
		return nil
	}

	data, err := tx.Serialize()
	if err != nil {
		return fmt.Errorf("failed to serialize transaction")
	}
	msgTx := new(wire.MsgTx)
	msgTx.Deserialize(bytes.NewBuffer(data))

	for i, utxo := range tx.UTXOs() {
		engine, err := txscript.NewEngine(utxo.ScriptPubKey(), msgTx, i,
			txscript.StandardVerifyFlags, txscript.NewSigCache(10),
			txscript.NewTxSigHashes(msgTx), int64(utxo.Amount()))
		if err != nil {
			return err
		}
		if err := engine.Execute(); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) SuggestGasPrice(ctx context.Context, speed types.TxSpeed, txSizeInBytes int) btctypes.Amount {
	gasStationPrice, err := c.gasStation.GasRequired(ctx, speed, txSizeInBytes)
	if err == nil {
		return gasStationPrice
	}
	c.logger.Errorf("error getting btc gas information: %v", err)
	c.logger.Infof("using 10k sats as gas price")
	return 10000 * btctypes.SAT
}

func (c *client) SerializePublicKey(pubkey ecdsa.PublicKey) []byte {
	return btctypes.SerializePublicKey(pubkey)
}

func (c *client) AddressFromBase58(addr string) (btctypes.Address, error) {
	return btctypes.AddressFromBase58(addr, c.network)
}
func (c *client) AddressFromPubKey(pubkey ecdsa.PublicKey) (btctypes.Address, error) {
	return btctypes.AddressFromPubKey(pubkey, c.network)
}
func (c *client) AddressFromScript(script []byte) (btctypes.Address, error) {
	return btctypes.AddressFromScript(script, c.network)
}

func (c *client) PayToAddrScript(address btctypes.Address) ([]byte, error) {
	return btctypes.PayToAddrScript(address, c.network)
}

type ErrInvalidTxHash struct {
	msg string
}

func NewErrInvalidTxHash(err error) error {
	return ErrInvalidTxHash{
		msg: fmt.Sprintf("invalid tx hash: %v", err),
	}
}

func (e ErrInvalidTxHash) Error() string {
	return e.msg
}

type ErrTxHashNotFound struct {
	msg string
}

func NewErrTxHashNotFound(err error) error {
	return ErrTxHashNotFound{
		msg: fmt.Sprintf("tx hash not found: %v", err),
	}
}
func (e ErrTxHashNotFound) Error() string {
	return e.msg
}

type ErrUTXOSpent struct {
	msg string
}

func NewErrUTXOSpent(err error) error {
	return ErrUTXOSpent{
		msg: fmt.Sprintf("utxo spent or invalid index: %v", err),
	}
}

func (e ErrUTXOSpent) Error() string {
	return e.msg
}

func (c *BtcClient) SegWitAddressFromPubKey(pubkey ecdsa.PublicKey) (btctypes.Address, error) {
	return btctypes.SegWitAddressFromPubKey(pubkey, c.Network())
}
func (c *BtcClient) SegWitAddressFromScript(script []byte) (btctypes.Address, error) {
	return btctypes.SegWitAddressFromScript(script, c.Network())
}
