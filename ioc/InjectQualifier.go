package ioc

import (
	"reflect"
	"sync"

	"github.com/go-errr/go/err"
	"github.com/go-errr/go/lang"
)

type InjectQualifier[T any] struct {
	fieldName string
	t         reflect.Type
	name      string
	optional  bool
}

func newInjectQualifier[T any]() *InjectQualifier[T] {
	return &InjectQualifier[T]{
		t: lang.TypeOf[T]()}
}

func (this *InjectQualifier[T]) Name(name string) *InjectQualifier[T] {
	this.name = name
	return this
}

func (this *InjectQualifier[T]) Optional() *InjectQualifier[T] {
	this.optional = true
	return this
}

func (this *InjectQualifier[T]) resolveOrExit() func() T {
	var instance T
	var once sync.Once
	return func() T {
		once.Do(func() {
			defer err.Recover(func(e any) {
				applicationContextInstance().exit1(e, "Cannot resolve bean.")
			})
			instance = this.doResolve()
		})
		return instance
	}
}

func (this *InjectQualifier[T]) resolve() func() T {
	var instance T
	var once sync.Once
	return func() T {
		once.Do(func() {
			instance = this.doResolve()
		})
		return instance
	}
}

func (this *InjectQualifier[T]) doResolve() T {
	var instance T
	raw := applicationContextInstance().bean(&InjectQualifier[any]{
		fieldName: this.fieldName,
		t:         this.t,
		name:      this.name,
		optional:  this.optional,
	})
	if raw != nil {
		val, ok := raw.(T)
		lang.Assert(ok, "Cannot cast bean to expected type %v; got %T", this.t, raw)
		instance = val
	}
	return instance
}
