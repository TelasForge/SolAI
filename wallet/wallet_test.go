package wallet

import (
	"strings"
	"testing"
)

func TestGenerateSeedPhrase(t *testing.T) {
	phrase, err := generateSeedPhrase()
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if phrase == "" {
		t.Error("Expected a non-empty phrase")
	}

	words := strings.Split(phrase, " ")
	if len(words) != 24 {
		t.Errorf("Expected 24 words, got %d", len(words))
	}
}

func TestSeedFromPhrase(t *testing.T) {
	phrase, err := generateSeedPhrase()
	if err != nil {
		t.Fatalf("Failed to generate phrase: %v", err)
	}

	seed, err := seedFromPhrase(phrase)
	if err != nil {
		t.Fatalf("Failed to get seed from phrase: %v", err)
	}
	if seed == nil {
		t.Error("Expected non-nil seed")
	}

	if len(seed) != 32 {
		t.Errorf("Expected seed length 32, got %d", len(seed))
	}
}

func TestCreateWallet(t *testing.T) {
	// A valid 24-word mnemonic for testing
	validPhrase := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"

	tests := []struct {
		name      string
		phrase    string
		expectErr bool
	}{
		{
			name:      "Empty phrase (generate new)",
			phrase:    "",
			expectErr: false,
		},
		{
			name:      "Valid phrase",
			phrase:    validPhrase,
			expectErr: false,
		},
		{
			name:      "Invalid phrase",
			phrase:    "invalid phrase",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kp, err := CreateWallet(tt.phrase)
			if (err != nil) != tt.expectErr {
				t.Errorf("CreateWallet() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if !tt.expectErr {
				if len(kp.publicKey) != 32 {
					t.Errorf("Expected public key length 32, got %d", len(kp.publicKey))
				}
				if len(kp.privateKey) != 64 {
					t.Errorf("Expected private key length 64, got %d", len(kp.privateKey))
				}
				if kp.Base58PubKey() == "" {
					t.Error("Expected non-empty Base58 public key")
				}
			}
		})
	}
}
