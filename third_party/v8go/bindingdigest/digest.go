// Package bindingdigest identifies the native sources the prebuilt darwin
// binding archives were compiled from. It is pure Go so the archive builder
// can use it before any archive exists.
package bindingdigest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// SourceDigest is the SHA-256 over the native binding sources
// (every top-level .cc and .h file, by name and content) that the prebuilt
// darwin archives in lib/ were compiled from. Artemis' cmd/artemis-v8go-binding
// writes it to lib/BINDING.sha256 when it builds the archives.
func SourceDigest(sources fs.FS) (string, error) {
	entries, err := fs.ReadDir(sources, ".")
	if err != nil {
		return "", err
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && (path.Ext(name) == ".cc" || path.Ext(name) == ".h") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := fs.ReadFile(sources, name)
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// Recorded parses the digest line of lib/BINDING.sha256.
func Recorded(manifest []byte) string {
	fields := strings.Fields(string(manifest))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
