package wallet

import (
	"crypto/ed25519"
	"fmt"
	"log/slog"
	"os"

	"github.com/tyler-smith/go-bip39"
)

func generateSeedPhrase() (string, error) {
	entropy, err := bip39.NewEntropy(256)
	if err != nil {
		return "", err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", err
	}
	return mnemonic, nil
}

func seedFromPhrase(phrase string) ([]byte, error) {
	return bip39.EntropyFromMnemonic(phrase)
}

func createWalletFromSeed(seed []byte) (ed25519.PrivateKey, ed25519.PublicKey) {
	privateKey := ed25519.NewKeyFromSeed(seed)
	return privateKey, privateKey.Public().(ed25519.PublicKey)
}

func CreateWallet(phrase string) (SolKeyPair, error) {
	if phrase != "" {
		seed, err := seedFromPhrase(phrase)
		if err != nil {
			return SolKeyPair{}, fmt.Errorf("unable to generate seed from phrase %w", err)
		}
		prv, pub := createWalletFromSeed(seed)
		return SolKeyPair{privateKey: prv, publicKey: pub}, nil
	}

	slog.Warn("No seed was passed to create wallet, generating a new wallet..")

	mnemonic, err := generateSeedPhrase()
	if err != nil {
		return SolKeyPair{}, fmt.Errorf("unable to generate key phrase %w", err)
	}

	seed, err := seedFromPhrase(mnemonic)
	if err != nil {
		return SolKeyPair{}, fmt.Errorf("unable to generate seed from phrase %w", err)
	}

	prv, pub := createWalletFromSeed(seed)

	wallet := SolKeyPair{privateKey: prv, publicKey: pub}

	slog.Info("generated wallet", "publicKey", wallet.Base58PubKey())

	// Write to stderr: stdout is reserved for the runtime's JSON event stream.
	fmt.Fprint(os.Stderr, "---- Please store the following seed phrase securely and pass it in the next time SolAI is started --- \n\n")
	fmt.Fprintf(os.Stderr, "%s\n", mnemonic)

	return wallet, nil
}
