package compiler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

var (
	ErrInvalidOrderedWorkerLimit = errors.New("invalid ordered worker limit")
	ErrNilOrderedWorker          = errors.New("ordered worker is nil")
	ErrNilOrderedCommitter       = errors.New("ordered committer is nil")
	ErrInvalidWorkSequence       = errors.New("invalid ordered work sequence")
)

// OrderedWorkItem is one task-shaped unit of work. Sequence defines the
// deterministic commit order and must be unique within one executor run.
type OrderedWorkItem[I any] struct {
	Sequence int
	TaskID   string
	Input    I
}

// OrderedWorkResult is passed to the ordered committer after a worker succeeds.
type OrderedWorkResult[O any] struct {
	Sequence int
	TaskID   string
	Output   O
}

type OrderedWorkerFunc[I, O any] func(context.Context, OrderedWorkItem[I]) (O, error)

type OrderedCommitFunc[O any] func(context.Context, OrderedWorkResult[O]) error

type OrderedExecutorOptions struct {
	// WorkerLimit controls the number of worker goroutines. Zero means one.
	WorkerLimit int
}

// RunOrderedWork runs work items with bounded goroutines, then commits only the
// contiguous successful prefix in deterministic sequence order.
func RunOrderedWork[I, O any](
	ctx context.Context,
	items []OrderedWorkItem[I],
	opts OrderedExecutorOptions,
	worker OrderedWorkerFunc[I, O],
	commit OrderedCommitFunc[O],
) error {
	ctx = contextOrBackground(ctx)
	limit, err := effectiveOrderedWorkerLimit(opts.WorkerLimit)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	if worker == nil {
		return ErrNilOrderedWorker
	}
	if commit == nil {
		return ErrNilOrderedCommitter
	}

	ordered, err := normalizeOrderedWorkItems(items)
	if err != nil {
		return err
	}
	if limit > len(ordered) {
		limit = len(ordered)
	}

	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan orderedQueuedWork[I])
	results := make(chan orderedWorkerResult[I, O], len(ordered))

	var firstErrMu sync.Mutex
	var firstErr error
	setFirstErr := func(err error) {
		if err == nil {
			return
		}
		firstErrMu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		firstErrMu.Unlock()
	}
	getFirstErr := func() error {
		firstErrMu.Lock()
		defer firstErrMu.Unlock()
		return firstErr
	}

	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for queued := range jobs {
				if err := execCtx.Err(); err != nil {
					results <- orderedWorkerResult[I, O]{position: queued.position, item: queued.item, err: err}
					continue
				}
				output, err := worker(execCtx, queued.item)
				if err != nil {
					setFirstErr(err)
				}
				results <- orderedWorkerResult[I, O]{position: queued.position, item: queued.item, output: output, err: err}
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, queued := range ordered {
			select {
			case <-execCtx.Done():
				return
			case jobs <- queued:
			}
		}
	}()

	wg.Wait()
	close(results)

	byPosition := make(map[int]orderedWorkerResult[I, O], len(ordered))
	for result := range results {
		byPosition[result.position] = result
	}

	for _, queued := range ordered {
		result, ok := byPosition[queued.position]
		if !ok {
			if err := getFirstErr(); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("ordered executor: missing result for sequence %d", queued.item.Sequence)
		}
		if result.err != nil {
			return result.err
		}
		if err := commit(ctx, OrderedWorkResult[O]{
			Sequence: result.item.Sequence,
			TaskID:   result.item.TaskID,
			Output:   result.output,
		}); err != nil {
			return fmt.Errorf("commit ordered work sequence %d: %w", result.item.Sequence, err)
		}
	}

	return nil
}

type orderedQueuedWork[I any] struct {
	position int
	item     OrderedWorkItem[I]
}

type orderedWorkerResult[I, O any] struct {
	position int
	item     OrderedWorkItem[I]
	output   O
	err      error
}

func effectiveOrderedWorkerLimit(limit int) (int, error) {
	switch {
	case limit == 0:
		return 1, nil
	case limit < 0:
		return 0, fmt.Errorf("%w: %d", ErrInvalidOrderedWorkerLimit, limit)
	default:
		return limit, nil
	}
}

func normalizeOrderedWorkItems[I any](items []OrderedWorkItem[I]) ([]orderedQueuedWork[I], error) {
	ordered := make([]orderedQueuedWork[I], len(items))
	for i, item := range items {
		ordered[i] = orderedQueuedWork[I]{position: i, item: item}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].item.Sequence < ordered[j].item.Sequence
	})
	for i, queued := range ordered {
		if queued.item.Sequence < 0 {
			return nil, fmt.Errorf("%w: negative sequence %d", ErrInvalidWorkSequence, queued.item.Sequence)
		}
		if i > 0 && queued.item.Sequence == ordered[i-1].item.Sequence {
			return nil, fmt.Errorf("%w: duplicate sequence %d", ErrInvalidWorkSequence, queued.item.Sequence)
		}
	}
	return ordered, nil
}
