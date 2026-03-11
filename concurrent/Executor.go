package concurrent

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"

	"github.com/go-external-config/go/util/concurrent"
	"github.com/go-external-config/go/util/err"
)

type Future[T any] interface {
	Get() T
	Result() (T, error)
}

type futureImpl[T any] struct {
	once   sync.Once
	result chan resultWrapper[T]
	val    T
	err    error
}

type resultWrapper[T any] struct {
	val T
	err error
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
		panic(f.err)
	}
	return f.val
}

func (f *futureImpl[T]) Result() (T, error) {
	f.wait()
	return f.val, f.err
}

var defaultExecutor *Executor[any]
var defaultExecutorMu sync.Mutex

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
		go func() {
			for job := range e.jobs {
				job()
			}
		}()
	}
	return e
}

func (e *Executor[T]) Submit(task func() T) Future[T] {
	f := &futureImpl[T]{result: make(chan resultWrapper[T], 1)}

	e.jobs <- func() {
		defer err.Recover(func(r any) {
			f.result <- resultWrapper[T]{err: fmt.Errorf("%v\n%s", r, debug.Stack())}
		})
		res := task()
		f.result <- resultWrapper[T]{val: res, err: nil}
	}

	return f
}

func (e *Executor[T]) Close() {
	close(e.jobs)
}
