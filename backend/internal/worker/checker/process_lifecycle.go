package checker

import (
	"context"
	"fmt"
	"time"
)

// Failures reports the first failure that requires replacing this worker.
func (w *Worker) Failures() <-chan error {
	return w.failures
}

// Degraded is latched until restart, even if a late process is eventually reaped.
func (w *Worker) Degraded() bool {
	select {
	case <-w.degraded:
		return true
	default:
		return false
	}
}

// Unreaped reports current count, oldest age, and the lifetime total.
func (w *Worker) Unreaped() (int, time.Duration, uint64) {
	w.processMu.Lock()
	defer w.processMu.Unlock()
	var oldest time.Duration
	for _, started := range w.unreaped {
		oldest = max(oldest, time.Since(started))
	}
	return len(w.unreaped), oldest, w.processes
}

func (w *Worker) beginAction(ctx context.Context) error {
	const location = "checker.Worker.beginAction"
	if w.Degraded() {
		return fmt.Errorf("%s - reject work after an unreaped process: %w", location, ErrWorkerUnreaped)
	}
	select {
	case <-w.degraded:
		return fmt.Errorf("%s - reject work while draining an unreaped process: %w", location, ErrWorkerUnreaped)
	case <-ctx.Done():
		return fmt.Errorf("%s - wait for checker action slot: %w", location, context.Cause(ctx))
	case w.actions <- struct{}{}:
	}
	// A pending caller can acquire a newly freed slot at the same instant the
	// degradation channel closes. Never admit a checker after that transition.
	if w.Degraded() {
		<-w.actions
		return fmt.Errorf("%s - reject work after capacity became degraded: %w", location, ErrWorkerUnreaped)
	}
	if w.observer != nil {
		w.observer.ActionStarted()
	}
	return nil
}

func (w *Worker) endAction(pendingReap <-chan struct{}) {
	const location = "checker.Worker.endAction"
	if pendingReap == nil {
		w.releaseAction()
		return
	}
	w.processMu.Lock()
	w.processes++
	id := w.processes
	w.unreaped[id] = time.Now()
	w.processMu.Unlock()
	w.degrade.Do(func() {
		close(w.degraded)
		w.failures <- fmt.Errorf("%s - await checker process reap: %w", location, ErrWorkerUnreaped)
		if w.options.OnDegraded != nil {
			w.options.OnDegraded()
		}
	})
	// This wait intentionally outlives the workflow. Its occupied action slot
	// prevents reuse, and degraded admission bounds these waiters by slot count.
	go func() {
		<-pendingReap
		w.processMu.Lock()
		delete(w.unreaped, id)
		w.processMu.Unlock()
		w.releaseAction()
	}()
}

func (w *Worker) releaseAction() {
	if w.observer != nil {
		w.observer.ActionEnded()
	}
	<-w.actions
}
