package api

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/solean/ponder/internal/ai"
)

type synchronizedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *synchronizedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(payload)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func TestStreamAIGenerationKeepsSilentProviderConnectionAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var output synchronizedBuffer
	var flushes atomic.Int32
	releaseProvider := make(chan struct{})
	result := make(chan aiGenerationResult, 1)
	go func() {
		generation, err := streamAIGeneration(ctx, &output, func() {
			flushes.Add(1)
		}, 5*time.Millisecond, func(onDelta func(string)) (ai.GenerationResult, error) {
			onDelta("partial")
			<-releaseProvider
			return ai.GenerationResult{
				Content: "final",
				Usage:   ai.TokenUsage{InputTokens: 12, OutputTokens: 4},
			}, nil
		})
		result <- aiGenerationResult{generation: generation, err: err}
	}()

	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	check := time.NewTicker(time.Millisecond)
	defer check.Stop()
	for !strings.Contains(output.String(), ": keep-alive\n\n") {
		select {
		case <-deadline.C:
			t.Fatalf("stream never emitted heartbeat; output = %q", output.String())
		case <-check.C:
		}
	}
	close(releaseProvider)

	select {
	case generated := <-result:
		if generated.err != nil {
			t.Fatalf("stream generation: %v", generated.err)
		}
		if generated.generation.Content != "final" {
			t.Fatalf("content = %q, want final", generated.generation.Content)
		}
		if generated.generation.Usage.InputTokens != 12 || generated.generation.Usage.OutputTokens != 4 {
			t.Fatalf("usage = %+v, want provider accounting", generated.generation.Usage)
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not return after provider completed")
	}

	stream := output.String()
	for _, expected := range []string{
		": connected\n\n",
		"event: delta\ndata: \"partial\"\n\n",
		": keep-alive\n\n",
	} {
		if !strings.Contains(stream, expected) {
			t.Errorf("stream missing %q: %q", expected, stream)
		}
	}
	if flushes.Load() < 3 {
		t.Fatalf("flush count = %d, want at least 3", flushes.Load())
	}
}
