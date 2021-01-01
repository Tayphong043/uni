package main

import (
	"testing"
)

func BenchmarkFormat(b *testing.B) {
	f := NewFormat("%(a) %(b l:auto) %(c)", false)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		f.Line(map[string]string{
			"a": "col a",
			"b": "col b",
			"c": "col c",
		})
	}
}
