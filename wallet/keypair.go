package wallet

import (
	"crypto/ed25519"

	"github.com/mr-tron/base58"
)

type SolKeyPair struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

func (kp SolKeyPair) Base58PubKey() string {
	return base58.Encode(kp.publicKey)
}

// PrivateKeyBytes returns the raw ed25519 private key bytes (64 bytes: seed + public key).
// This is intentionally restricted to package-level consumers that need to sign transactions.
func (kp SolKeyPair) PrivateKeyBytes() []byte {
	return []byte(kp.privateKey)
}
