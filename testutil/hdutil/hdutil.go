package hdutil

import (
	"crypto/ecdsa"

	"github.com/btcsuite/btcutil/hdkeychain"
	"github.com/renproject/mercury/types/btctypes"
	"github.com/tyler-smith/go-bip39"
)

func DerivePrivKey(key *hdkeychain.ExtendedKey, path ...uint32) (*ecdsa.PrivateKey, error) {
	var err error
	for _, val := range path {
		key, err = key.Child(val)
		if err != nil {
			return nil, err
		}
	}
	privKey, err := key.ECPrivKey()
	if err != nil {
		return nil, err
	}
	return privKey.ToECDSA(), nil
}

func DeriveExtendedPrivKey(mnemonic, passphrase string, network btctypes.Network) (*hdkeychain.ExtendedKey, error) {
	seed := DeriveSeed(mnemonic, passphrase)
	key, err := hdkeychain.NewMaster(seed, network.Params())
	if err != nil {
		return nil, err
	}
	return key, nil
}

func DeriveSeed(mnemonic, passphrase string) []byte {
	return bip39.NewSeed(mnemonic, passphrase)
}
