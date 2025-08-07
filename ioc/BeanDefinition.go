package ioc

import (
	"fmt"
	"reflect"

	"github.com/go-external-config/go/lang"
)

type Scope int
type Profile string

const (
	Singleton Scope = iota
	Prototype
)

type BeanDefinition interface {
	getType() reflect.Type
	getName() string
	isPrimary() bool
	getScope() Scope
	instantiate() any
	getInstance() any
	String() string
}

type BeanDefinitionImpl[T any] struct {
	t                   reflect.Type
	name                string
	scope               Scope
	primary             bool
	factoryMethod       func() *T
	postConstructMethod func(*T)
	preDestroyMethod    func(*T)
	instance            any
}

func newBeanDefinition[T any]() *BeanDefinitionImpl[T] {
	return &BeanDefinitionImpl[T]{
		t: lang.TypeOf[T](),
	}
}

// Set optional name
func (b *BeanDefinitionImpl[T]) Name(name string) *BeanDefinitionImpl[T] {
	b.name = name
	return b
}

// Set optional scope
func (b *BeanDefinitionImpl[T]) Scope(scope string) *BeanDefinitionImpl[T] {
	switch scope {
	case "singleton":
		b.scope = Singleton
	case "prototype":
		b.scope = Prototype
	default:
		panic(fmt.Sprintf("%s scope not supported", scope))
	}
	return b
}

// Mark this bean as primary
func (b *BeanDefinitionImpl[T]) Primary() *BeanDefinitionImpl[T] {
	b.primary = true
	return b
}

// Set the factory method reference or anonymous function with actual implementation
func (b *BeanDefinitionImpl[T]) Factory(f func() *T) *BeanDefinitionImpl[T] {
	b.factoryMethod = f
	return b
}

// It is safe to use injected beans at this point
func (b *BeanDefinitionImpl[T]) PostConstruct(f func(*T)) *BeanDefinitionImpl[T] {
	b.postConstructMethod = f
	return b
}

// Clean-up resources before shutdown. Is not called on prototype beans.
func (b *BeanDefinitionImpl[T]) PreDestroy(f func(*T)) *BeanDefinitionImpl[T] {
	b.preDestroyMethod = f
	return b
}

// Register the bean within the context
func (b *BeanDefinitionImpl[T]) Register() {
	lang.AssertState(b.factoryMethod != nil, "Bean factory method must be provided")
	ApplicationContextInstance().Register(b)
}

// Implements BeanDefinition
func (b *BeanDefinitionImpl[T]) getType() reflect.Type {
	return b.t
}

func (b *BeanDefinitionImpl[T]) getName() string {
	return b.name
}

func (b *BeanDefinitionImpl[T]) isPrimary() bool {
	return b.primary
}

func (b *BeanDefinitionImpl[T]) getScope() Scope {
	return b.scope
}

func (b *BeanDefinitionImpl[T]) instantiate() any {
	b.instance = b.factoryMethod()
	if b.postConstructMethod != nil {
		b.postConstructMethod(b.instance.(*T))
	}
	return b.instance
}

func (b *BeanDefinitionImpl[T]) getInstance() any {
	return b.instance
}

// Implements String
func (b *BeanDefinitionImpl[T]) String() string {
	return fmt.Sprintf("%s[%s%s%s]", b.t, lang.If(b.scope == Singleton, "singleton", "prototype"), lang.If(b.primary, " primary", ""), lang.If(len(b.name) > 0, " "+b.name, ""))
}
