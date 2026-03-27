package telemetry

import (
	"context"
	"time"

	"github.com/sazid/bitcode/internal"
	"github.com/sazid/bitcode/internal/guard"
)

// guardWrapper instruments a guard.GuardEvaluator with telemetry.
type guardWrapper struct {
	inner    guard.GuardEvaluator
	observer Observer
	turn     *TurnCounter
}

// WrapGuardEvaluator wraps a GuardEvaluator with telemetry instrumentation.
func WrapGuardEvaluator(g guard.GuardEvaluator, obs Observer, turn *TurnCounter) guard.GuardEvaluator {
	return &guardWrapper{inner: g, observer: obs, turn: turn}
}

func (w *guardWrapper) Evaluate(ctx context.Context, toolName, input string, eventsCh chan<- internal.Event) (*guard.Decision, error) {
	start := time.Now()
	decision, err := w.inner.Evaluate(ctx, toolName, input, eventsCh)
	duration := time.Since(start)

	ev := GuardEvent{
		ToolName: toolName,
		Duration: duration,
	}
	if err != nil {
		ev.Error = err.Error()
	}
	if decision != nil {
		ev.Verdict = string(decision.Verdict)
	}
	w.observer.RecordGuard(w.turn.Get(), ev)
	return decision, err
}
