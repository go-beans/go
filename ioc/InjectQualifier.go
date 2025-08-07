package ioc

import (
	"reflect"
	"sync"

	"github.com/go-external-config/go/lang"
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
			raw := ApplicationContextInstance().Bean(&InjectQualifier[any]{
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
