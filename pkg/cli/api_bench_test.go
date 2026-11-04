package cli

import "testing"

// BenchmarkNewRootCmd guards the startup budget (< 50ms/op): the spec is
// parsed and the api tree built on every invocation.
func BenchmarkNewRootCmd(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewRootCmd("bench", "0.0.0")
	}
}
