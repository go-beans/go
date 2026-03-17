package concurrent

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/util/concurrent"
)

type Future[T any] interface {
	Get() T
	Result() (T, error)
}

type futureImpl[T any] struct {
	once   sync.Once
	result chan resultWrapper[T]
	val    T
	err    any
}

type resultWrapper[T any] struct {
	val T
	err any
}

func (f *futureImpl[T]) wait() {
	f.once.Do(func() {
		r := <-f.result
		f.val, f.err = r.val, r.err
	})
}

func (f *futureImpl[T]) Get() T {
	f.wait()
	if f.err != nil {
		panic(NewExecutionExceptionFrom(fmt.Sprint(f.err), f.err))
	}
	return f.val
}

func (f *futureImpl[T]) Result() (T, error) {
	f.wait()
	if f.err == nil {
		return f.val, nil
	} else {
		return f.val, NewExecutionExceptionFrom(fmt.Sprint(f.err), f.err)
	}
}

var defaultExecutor *Executor[any]
var defaultExecutorMu sync.Mutex

// Executor is a fixed-size worker pool for asynchronous task execution.
//
// A configured number of worker goroutines is started eagerly and
// tasks are distributed among them. Task submission uses synchronous
// handoff via an unbuffered channel. If all workers are busy, Submit()
// blocks until a worker becomes available. Tasks are never executed
// by the caller goroutine.
//
// Each submitted task returns a Future that allows waiting for the
// result. Task panics are recovered and converted into an execution
// error. Future.Get() panics on failure, while Future.Result()
// returns (value, error).
//
// The executor provides natural backpressure and prevents unbounded
// task queuing. Cancellation, timeouts, and interruption are not
// supported.
//
// Close() stops the executor by closing the job channel. Workers exit
// after processing already accepted tasks. Submitting after Close()
// causes a panic.
type Executor[T any] struct {
	jobs chan func()
}

func DefaultExecutor() *Executor[any] {
	if defaultExecutor == nil {
		concurrent.Synchronized(&defaultExecutorMu, func() {
			if defaultExecutor == nil {
				defaultExecutor = NewExecutor[any](runtime.NumCPU())
			}
		})
	}
	return defaultExecutor
}

func NewExecutor[T any](workers int) *Executor[T] {
	e := &Executor[T]{jobs: make(chan func())}
	for i := 0; i < workers; i++ {
		e.runWorker()
	}
	return e
}

func (e *Executor[T]) Submit(task func() T) Future[T] {
	f := &futureImpl[T]{result: make(chan resultWrapper[T], 1)}

	e.jobs <- func() {
		defer err.Recover(func(r any) {
			f.result <- resultWrapper[T]{err: r}
		})
		res := task()
		f.result <- resultWrapper[T]{val: res, err: nil}
	}

	return f
}

func (e *Executor[T]) runWorker() {
	go func() {
		for job := range e.jobs {
			job()
		}
	}()
}

func (e *Executor[T]) Close() {
	close(e.jobs)
}
