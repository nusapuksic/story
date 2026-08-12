package compiler

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunOrderedWorkSerialExecution(t *testing.T) {
	items := []OrderedWorkItem[int]{
		{Sequence: 2, TaskID: "task-2", Input: 2},
		{Sequence: 0, TaskID: "task-0", Input: 0},
		{Sequence: 1, TaskID: "task-1", Input: 1},
	}
	var active atomic.Int32
	var maxActive atomic.Int32
	var started []int
	var committed []int

	err := RunOrderedWork(context.Background(), items, OrderedExecutorOptions{}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		current := active.Add(1)
		updateMaxActive(&maxActive, current)
		defer active.Add(-1)
		started = append(started, item.Sequence)
		return item.Input, nil
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		committed = append(committed, result.Output)
		return nil
	})
	if err != nil {
		t.Fatalf("RunOrderedWork: %v", err)
	}
	if maxActive.Load() > 1 {
		t.Fatalf("max active = %d, want <= 1", maxActive.Load())
	}
	if !reflect.DeepEqual(started, []int{0, 1, 2}) {
		t.Fatalf("started = %v, want [0 1 2]", started)
	}
	if !reflect.DeepEqual(committed, []int{0, 1, 2}) {
		t.Fatalf("committed = %v, want [0 1 2]", committed)
	}
}

func TestRunOrderedWorkWorkerLimit(t *testing.T) {
	items := makeOrderedIntItems(8)
	var active atomic.Int32
	var maxActive atomic.Int32

	err := RunOrderedWork(context.Background(), items, OrderedExecutorOptions{WorkerLimit: 3}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		current := active.Add(1)
		updateMaxActive(&maxActive, current)
		defer active.Add(-1)
		time.Sleep(10 * time.Millisecond)
		return item.Input, nil
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		return nil
	})
	if err != nil {
		t.Fatalf("RunOrderedWork: %v", err)
	}
	if got := maxActive.Load(); got > 3 {
		t.Fatalf("max active = %d, want <= 3", got)
	}
	if got := maxActive.Load(); got < 2 {
		t.Fatalf("max active = %d, want concurrent fake workers", got)
	}
}

func TestRunOrderedWorkOutOfOrderCompletionCommitsInOrder(t *testing.T) {
	items := makeOrderedIntItems(5)
	var committed []int

	err := RunOrderedWork(context.Background(), items, OrderedExecutorOptions{WorkerLimit: 5}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		time.Sleep(time.Duration(5-item.Sequence) * 5 * time.Millisecond)
		return item.Input, nil
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		committed = append(committed, result.Output)
		return nil
	})
	if err != nil {
		t.Fatalf("RunOrderedWork: %v", err)
	}
	if !reflect.DeepEqual(committed, []int{0, 1, 2, 3, 4}) {
		t.Fatalf("committed = %v, want ordered outputs", committed)
	}
}

func TestRunOrderedWorkCommitsReadyPrefixBeforeAllWorkersFinish(t *testing.T) {
	items := makeOrderedIntItems(2)
	releaseSecond := make(chan struct{})
	secondStarted := make(chan struct{})
	committed := make(chan int, 2)
	done := make(chan error, 1)

	go func() {
		done <- RunOrderedWork(context.Background(), items, OrderedExecutorOptions{WorkerLimit: 2}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
			if item.Sequence == 1 {
				close(secondStarted)
				select {
				case <-releaseSecond:
				case <-ctx.Done():
					return 0, ctx.Err()
				}
			}
			return item.Input, nil
		}, func(ctx context.Context, result OrderedWorkResult[int]) error {
			committed <- result.Output
			return nil
		})
	}()

	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second worker did not start")
	}

	select {
	case got := <-committed:
		if got != 0 {
			t.Fatalf("first streamed commit = %d, want 0", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first result was not committed before later worker finished")
	}

	close(releaseSecond)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunOrderedWork: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("RunOrderedWork did not finish")
	}

	select {
	case got := <-committed:
		if got != 1 {
			t.Fatalf("second streamed commit = %d, want 1", got)
		}
	default:
		t.Fatal("missing second commit")
	}
}
func TestRunOrderedWorkCommitsContiguousPrefixBeforeFailure(t *testing.T) {
	boom := errors.New("boom")
	items := makeOrderedIntItems(5)
	var committed []int

	err := RunOrderedWork(context.Background(), items, OrderedExecutorOptions{WorkerLimit: 5}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		switch item.Sequence {
		case 0, 1:
			time.Sleep(time.Duration(item.Sequence+1) * 5 * time.Millisecond)
			return item.Input, nil
		case 2:
			time.Sleep(15 * time.Millisecond)
			return 0, boom
		default:
			return item.Input, nil
		}
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		committed = append(committed, result.Output)
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("RunOrderedWork error = %v, want boom", err)
	}
	if !reflect.DeepEqual(committed, []int{0, 1}) {
		t.Fatalf("committed = %v, want contiguous prefix [0 1]", committed)
	}
}

func TestRunOrderedWorkParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	items := makeOrderedIntItems(1)
	go func() {
		<-started
		cancel()
	}()

	err := RunOrderedWork(ctx, items, OrderedExecutorOptions{WorkerLimit: 1}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		close(started)
		<-ctx.Done()
		return 0, ctx.Err()
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		t.Fatal("commit should not run after cancellation")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOrderedWork error = %v, want context.Canceled", err)
	}
}

func TestRunOrderedWorkEmptyListSucceeds(t *testing.T) {
	if err := RunOrderedWork[int, int](context.Background(), nil, OrderedExecutorOptions{}, nil, nil); err != nil {
		t.Fatalf("RunOrderedWork empty list: %v", err)
	}
}

func TestRunOrderedWorkInvalidWorkerLimit(t *testing.T) {
	err := RunOrderedWork(context.Background(), makeOrderedIntItems(1), OrderedExecutorOptions{WorkerLimit: -1}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		return item.Input, nil
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		return nil
	})
	if !errors.Is(err, ErrInvalidOrderedWorkerLimit) {
		t.Fatalf("RunOrderedWork error = %v, want ErrInvalidOrderedWorkerLimit", err)
	}
}

func TestRunOrderedWorkRejectsDuplicateSequences(t *testing.T) {
	items := []OrderedWorkItem[int]{{Sequence: 1}, {Sequence: 1}}
	err := RunOrderedWork(context.Background(), items, OrderedExecutorOptions{}, func(ctx context.Context, item OrderedWorkItem[int]) (int, error) {
		return item.Input, nil
	}, func(ctx context.Context, result OrderedWorkResult[int]) error {
		return nil
	})
	if !errors.Is(err, ErrInvalidWorkSequence) {
		t.Fatalf("RunOrderedWork error = %v, want ErrInvalidWorkSequence", err)
	}
}

func makeOrderedIntItems(n int) []OrderedWorkItem[int] {
	items := make([]OrderedWorkItem[int], n)
	for i := 0; i < n; i++ {
		items[i] = OrderedWorkItem[int]{Sequence: i, TaskID: "task", Input: i}
	}
	return items
}

func updateMaxActive(max *atomic.Int32, current int32) {
	for {
		observed := max.Load()
		if current <= observed || max.CompareAndSwap(observed, current) {
			return
		}
	}
}
