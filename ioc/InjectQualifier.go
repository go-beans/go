package ioc

import (
	"reflect"
	"sync"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/lang"
)

type InjectQualifier[T any] struct {
	t        reflect.Type
	name     string
	optional bool
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

func (this *InjectQualifier[T]) resolve() func() T {
	var once sync.Once
	var instance T
	return func() T {
		once.Do(func() {
			defer err.Recover(func(e any) {
				applicationContextInstance().doExitPrintStackTrace(e, "Cannot resolve dependency.")
			})
			raw := applicationContextInstance().Bean(&InjectQualifier[any]{
				t:        this.t,
				name:     this.name,
				optional: this.optional,
			})
			if raw != nil {
				val, ok := raw.(T)
				lang.Assert(ok, "Cannot cast bean to expected type %v; got %T", this.t, raw)
				instance = val
			}
		})
		return instance
	}
}
