package capture

import (
	"context"
	"slices"
	"testing"
)

type archiveFunc func(context.Context, string) []string

func (f archiveFunc) Candidates(ctx context.Context, raw string) []string { return f(ctx, raw) }

func TestSourcesDiscoverLazilyAndBoundUniqueArchives(t *testing.T) {
	const original = "https://example.com/article"
	renderer := &fakeRenderer{}
	discoveries := 0
	c := New(renderer, archiveFunc(func(context.Context, string) []string {
		discoveries++
		return []string{original, "", "https://archive.example/1", "https://archive.example/1", "https://archive.example/2", "https://archive.example/3", "https://archive.example/4"}
	}))
	for range c.Sources(context.Background(), original) {
		break
	}
	if discoveries != 0 {
		t.Fatal("discovered archives before original was rejected")
	}
	renderer.calls = nil
	var got []string
	for source := range c.Sources(context.Background(), original) {
		got = append(got, source.URL)
	}
	want := []string{original, "https://archive.example/1", "https://archive.example/2", "https://archive.example/3"}
	if discoveries != 1 || !slices.Equal(got, want) || !slices.Equal(renderer.calls, want) {
		t.Fatalf("sources=%v requests=%v discoveries=%d", got, renderer.calls, discoveries)
	}
}

func TestSourcesCancellationStopsDiscoveryAndRetrieval(t *testing.T) {
	for _, before := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		renderer := &fakeRenderer{}
		c := New(renderer, archiveFunc(func(context.Context, string) []string { t.Fatal("discovered after cancellation"); return nil }))
		if before {
			cancel()
		}
		for range c.Sources(ctx, "https://example.com/article") {
			cancel()
		}
		cancel()
		want := 1
		if before {
			want = 0
		}
		if len(renderer.calls) != want {
			t.Fatalf("calls=%v", renderer.calls)
		}
	}
}
