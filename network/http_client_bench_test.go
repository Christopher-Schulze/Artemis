package network

import "testing"

// BenchmarkNewHTTPClient measures per-engine HTTP client construction, which
// runs once for every engine an embedding host or session creates.
func BenchmarkNewHTTPClient(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		client, err := NewHTTPClient(HTTPClientConfig{})
		if err != nil {
			b.Fatal(err)
		}
		if err := client.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
