package ioc

import (
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"runtime/debug"
	"sync"

	"github.com/go-external-config/go/lang"
	"github.com/go-external-config/go/util/err"
)

type InjectQualifier[T any] struct {
	t    reflect.Type
	name string
}

func newInjectQualifier[T any]() *InjectQualifier[T] {
	return &InjectQualifier[T]{
		t: lang.TypeOf[T]()}
}

func (i *InjectQualifier[T]) Name(name string) *InjectQualifier[T] {
	i.name = name
	return i
}

func (i *InjectQualifier[T]) resolve() func() T {
	var once sync.Once
	var instance T
	return func() T {
		once.Do(func() {
			defer err.Recover(func(err any) {
				slog.Error(fmt.Sprintf("%v\n%s", err, debug.Stack()))
				Close()
				os.Exit(1)
			})
			raw := applicationContextInstance().Bean(&InjectQualifier[any]{
				t:    i.t,
				name: i.name,
			})
			val, ok := raw.(T)
			lang.AssertState(ok, "Cannot cast bean to expected type %v; got %T", i.t, raw)
			instance = val
		})
		return instance
	}
}
