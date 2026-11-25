package demo

import "testing"

func BenchmarkRender(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		render()
	}
}
