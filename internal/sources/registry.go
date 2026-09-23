package sources

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	_ "embed"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/skills"
)

// OfficialIndex is the source types registry this server knows. It is read
// with the skills registry's code, but it is a registry of its own, signed
// with a key of its own, so a key that signs source types cannot publish a
// skill, and the other way round.
const OfficialIndex = "https://raw.githubusercontent.com/teanode/teanode-sources/main/index.json"

// OfficialPublisher is who the official registry is.
const OfficialPublisher = "github.com/teanode/teanode-sources"

//go:embed keys/teanode-sources-ed25519-public.pem
var publicKeyPEM []byte

// BuiltinKey is the official registry's signing key, as shipped.
func BuiltinKey() (ed25519.PublicKey, error) {
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, fmt.Errorf("sources: the built-in key is not a public key in PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("sources: the built-in key cannot be read: %w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("sources: the built-in key is %T, not an Ed25519 key", parsed)
	}
	return key, nil
}

// Registry is the official source types registry, or the one at an
// address with a key given in its place, for a test.
func Registry(address string, key ed25519.PublicKey) (*skills.Registry, error) {
	if strings.TrimSpace(address) == "" {
		address = OfficialIndex
	}
	if len(key) == 0 {
		builtin, err := BuiltinKey()
		if err != nil {
			return nil, err
		}
		key = builtin
	}
	return &skills.Registry{Address: address, PublicKey: key}, nil
}

// Entries is every type a registry lists, each checked against its key;
// one whose signature does not hold is left out.
func Entries(ctx context.Context, registry *skills.Registry) ([]*skills.Entry, error) {
	index, err := registry.Index(ctx)
	if err != nil {
		return nil, err
	}
	var entries []*skills.Entry
	for _, entry := range index.Sources {
		if registry.Verify(entry) == nil {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// Find is one type of a registry by name, already checked.
func Find(ctx context.Context, registry *skills.Registry, name string) (*skills.Entry, error) {
	index, err := registry.Index(ctx)
	if err != nil {
		return nil, err
	}
	for _, entry := range index.Sources {
		if strings.EqualFold(entry.Name, name) {
			if err := registry.Verify(entry); err != nil {
				return nil, err
			}
			return entry, nil
		}
	}
	return nil, fmt.Errorf("sources: the registry has no source type called %q", name)
}
