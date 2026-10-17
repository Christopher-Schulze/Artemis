// Package sourcepin hashes compiled source inputs without reading a checkout.
package sourcepin

import (
	"crypto/sha256"
	"embed"
	"fmt"
	"sort"
)

// Hash binds ordered immutable source names/bytes and additional identities.
// Source bytes remain internal and never enter a portable fixture payload.
func Hash(files embed.FS, identities ...string) (string, error) {
	entries, err := files.ReadDir(".")
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	hash := sha256.New()
	for _, identity := range identities {
		if _, err := fmt.Fprintf(hash, "identity:%d:%s\x00", len(identity), identity); err != nil {
			return "", err
		}
	}
	for _, entry := range entries {
		data, err := files.ReadFile(entry.Name())
		if err != nil {
			return "", err
		}
		if _, err := fmt.Fprintf(hash, "%s\x00%s\x00", entry.Name(), data); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
