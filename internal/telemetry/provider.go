package telemetry

import (
	"context"
	"time"

	"github.com/sazid/bitcode/internal/llm"
)

// providerWrapper instruments an llm.Provider with telemetry.
type providerWrapper struct {
	inner    llm.Provider
	observer Observer
	turn     *TurnCounter
	provider string // human-readable provider name
}

// statefulWrapper instruments an llm.StatefulProvider.
type statefulWrapper struct {
	providerWrapper
	inner llm.StatefulProvider
}

// sessionWrapper instruments an llm.SessionProvider.
type sessionWrapper struct {
	statefulWrapper
	inner llm.SessionProvider
}

// WrapProvider wraps a provider with telemetry instrumentation.
// It preserves the provider's type (Provider, StatefulProvider, SessionProvider)
// so type assertions in the agent loop continue to work.
func WrapProvider(p llm.Provider, obs Observer, turn *TurnCounter, providerName string) llm.Provider {
	base := providerWrapper{inner: p, observer: obs, turn: turn, provider: providerName}

	if sp, ok := p.(llm.SessionProvider); ok {
		return &sessionWrapper{
			statefulWrapper: statefulWrapper{providerWrapper: base, inner: sp},
			inner:           sp,
		}
	}
	if sp, ok := p.(llm.StatefulProvider); ok {
		return &statefulWrapper{providerWrapper: base, inner: sp}
	}
	return &base
}

func (w *providerWrapper) Complete(ctx context.Context, params llm.CompletionParams, onDelta func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	start := time.Now()
	resp, err := w.inner.Complete(ctx, params, onDelta)
	duration := time.Since(start)

	ev := LLMEvent{
		Model:    params.Model,
		Provider: w.provider,
		Duration: duration,
	}
	if err != nil {
		ev.Error = err.Error()
		w.observer.RecordLLM(w.turn.Get(), ev)
		return nil, err
	}
	ev.InputTokens = resp.Usage.InputTokens
	ev.OutputTokens = resp.Usage.OutputTokens
	ev.CacheRead = resp.Usage.CacheRead
	ev.CacheCreate = resp.Usage.CacheCreate
	ev.FinishReason = string(resp.FinishReason)
	w.observer.RecordLLM(w.turn.Get(), ev)
	return resp, nil
}

func (w *statefulWrapper) Complete(ctx context.Context, params llm.CompletionParams, onDelta func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	return w.providerWrapper.Complete(ctx, params, onDelta)
}

func (w *statefulWrapper) CompleteStateful(ctx context.Context, params llm.StatefulCompletionParams, onDelta func(llm.StreamDelta)) (*llm.StatefulCompletionResponse, error) {
	start := time.Now()
	resp, err := w.inner.CompleteStateful(ctx, params, onDelta)
	duration := time.Since(start)

	ev := LLMEvent{
		Model:    params.Model,
		Provider: w.providerWrapper.provider,
		Duration: duration,
	}
	if err != nil {
		ev.Error = err.Error()
		w.observer.RecordLLM(w.turn.Get(), ev)
		return nil, err
	}
	ev.InputTokens = resp.Usage.InputTokens
	ev.OutputTokens = resp.Usage.OutputTokens
	ev.CacheRead = resp.Usage.CacheRead
	ev.CacheCreate = resp.Usage.CacheCreate
	ev.FinishReason = string(resp.FinishReason)
	w.observer.RecordLLM(w.turn.Get(), ev)
	return resp, nil
}

func (w *sessionWrapper) Complete(ctx context.Context, params llm.CompletionParams, onDelta func(llm.StreamDelta)) (*llm.CompletionResponse, error) {
	return w.statefulWrapper.Complete(ctx, params, onDelta)
}

func (w *sessionWrapper) CompleteStateful(ctx context.Context, params llm.StatefulCompletionParams, onDelta func(llm.StreamDelta)) (*llm.StatefulCompletionResponse, error) {
	return w.statefulWrapper.CompleteStateful(ctx, params, onDelta)
}

func (w *sessionWrapper) Connect(ctx context.Context) error {
	return w.inner.Connect(ctx)
}

func (w *sessionWrapper) Close() error {
	return w.inner.Close()
}

func (w *sessionWrapper) IsConnected() bool {
	return w.inner.IsConnected()
}
