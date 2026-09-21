// Package identity holds the hub's key: generated on first start, never
// leaving the state directory. Enrollment copies only the public half.
package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

// Key is the hub's Ed25519 key pair in the forms the rest of the hub needs.
type Key struct {
	Signer ssh.Signer
	// AuthorizedKey is the single line a remote's authorized_keys accepts.
	AuthorizedKey string
}

// Load reads the key from stateDir, creating it on first start.
func Load(stateDir string) (*Key, error) {
	privPath := filepath.Join(stateDir, "hub_key")
	pubPath := filepath.Join(stateDir, "hub_key.pub")

	raw, err := os.ReadFile(privPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		raw, err = generate(privPath, pubPath)
		if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("read hub key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse hub key: %w", err)
	}
	return &Key{
		Signer:        signer,
		AuthorizedKey: string(ssh.MarshalAuthorizedKey(signer.PublicKey())),
	}, nil
}

func generate(privPath, pubPath string) ([]byte, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate hub key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "homedash hub")
	if err != nil {
		return nil, fmt.Errorf("encode hub key: %w", err)
	}
	pem := pemEncode(block.Type, block.Bytes)
	if err := os.WriteFile(privPath, pem, 0o600); err != nil {
		return nil, fmt.Errorf("write hub key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(pubPath, ssh.MarshalAuthorizedKey(sshPub), 0o644); err != nil {
		return nil, fmt.Errorf("write hub public key: %w", err)
	}
	return pem, nil
}
