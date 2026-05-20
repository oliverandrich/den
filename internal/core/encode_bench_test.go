package core

import "testing"

// Storage-seam encode benchmarks. Two shapes: tabular (no HTML chars,
// the no-escape change is allocation-pool-neutral) and markup-heavy
// (the no-escape change skips \uXXXX expansion of `&` / `<` / `>`).

func BenchmarkDBEncode_Tabular(b *testing.B) {
	db := &DB{}
	doc := map[string]any{
		"_id":   "01HQ3K8V2X4XR1KPMM6N4G8J3P",
		"name":  "Widget",
		"price": 9.99,
		"tags":  []string{"a", "b", "c"},
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = db.encode(doc)
	}
}

func BenchmarkDBEncode_MarkupHeavy(b *testing.B) {
	db := &DB{}
	doc := map[string]any{
		"_id":  "01HQ3K8V2X4XR1KPMM6N4G8J3P",
		"body": `<p>Hello & welcome to <a href="https://example.com/x?a=1&b=2">our site</a></p>`,
	}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = db.encode(doc)
	}
}
