package ioc

import (
	"reflect"

	"github.com/go-errr/go/err"
	"github.com/go-jang/go/lang"
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

func (this *InjectQualifier[T]) resolveOrExit() Provider[T] {
	return func() T {
		defer err.Recover(func(e any) {
			applicationContextInstance().exit1(e, "Failed to resolve bean.")
		})
		return this.doResolve()
	}
}

func (this *InjectQualifier[T]) resolve() Provider[T] {
	return func() T {
		return this.doResolve()
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
