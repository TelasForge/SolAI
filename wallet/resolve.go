package wallet

import "sync"

var (
	resolveMu sync.Mutex
	resolved  = map[string]SolKeyPair{}
)

// Resolve returns a keypair for the given seed, deriving it once per process and
// caching the result. Every tool in a session shares the same wallet.
func Resolve(seed string) (SolKeyPair, error) {
	resolveMu.Lock()
	defer resolveMu.Unlock()
	if kp, ok := resolved[seed]; ok {
		return kp, nil
	}
	kp, err := CreateWallet(seed)
	if err != nil {
		return SolKeyPair{}, err
	}
	resolved[seed] = kp
	return kp, nil
}
