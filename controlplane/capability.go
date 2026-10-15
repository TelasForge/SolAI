package controlplane

import (
	"errors"
	"fmt"
	"sort"

	"github.com/melodyogonna/solai/config"
)

// ErrUnknownCapability is returned when a session requests a capability the
// control plane does not know how to provision.
var ErrUnknownCapability = errors.New("unknown capability")

// Capability is an authority the control plane provisions into a session's
// environment.
//
// The runtime never sees this type — it only sees the resulting environment.
// This is where capabilities meet infrastructure: a real deployment maps each
// capability to sandbox policy (network egress, mounts, secrets) in addition
// to the environment variables provisioned here.
type Capability struct {
	Name        string
	Description string

	// env returns the environment assignments this capability grants, given the
	// control plane's own configuration.
	env func(config.Config) []string
}

var capabilities = []Capability{
	{
		Name:        "network",
		Description: "Outbound network access. Granted by the platform's sandbox.",
	},
	{
		Name:        "rpc",
		Description: "Solana RPC access.",
		env: func(c config.Config) []string {
			return []string{
				config.EnvRPCURL + "=" + c.RPCURL,
				config.EnvCommitment + "=" + c.Commitment,
			}
		},
	},
	{
		Name:        "wallet",
		Description: "Solana wallet signing.",
		env: func(c config.Config) []string {
			if c.WalletSeed == "" {
				return nil
			}
			return []string{config.EnvWalletSeed + "=" + c.WalletSeed}
		},
	},
}

// CapabilityNames returns the provisionable capability names, sorted.
func CapabilityNames() []string {
	out := make([]string, 0, len(capabilities))
	for _, c := range capabilities {
		out = append(out, c.Name)
	}
	sort.Strings(out)
	return out
}

// provision returns the environment assignments granted by the named
// capabilities. Unknown names are an error.
func provision(names []string, cfg config.Config) ([]string, error) {
	byName := make(map[string]Capability, len(capabilities))
	for _, c := range capabilities {
		byName[c.Name] = c
	}

	seen := make(map[string]bool, len(names))
	var env []string
	for _, name := range names {
		c, ok := byName[name]
		if !ok {
			return nil, fmt.Errorf("%w %q", ErrUnknownCapability, name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		if c.env != nil {
			env = append(env, c.env(cfg)...)
		}
	}
	return env, nil
}
