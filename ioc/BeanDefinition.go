package ioc

import (
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"

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
	getNames() []string
	isPrimary() bool
	getScope() Scope
	getProfiles() []string
	instantiate() any
	getInstance() any
	postConstruct()
	preDestroyEligible() bool
	preDestroy()
	getMutex() *sync.Mutex
	String() string
}

type BeanDefinitionImpl[T any] struct {
	t                   reflect.Type
	names               []string
	scope               Scope
	primary             bool
	profiles            []string
	factoryMethod       func() T
	postConstructMethod func(T)
	preDestroyMethod    func(T)
	instance            any
	mutex               sync.Mutex
}

func newBeanDefinition[T any]() *BeanDefinitionImpl[T] {
	return &BeanDefinitionImpl[T]{
		t: lang.TypeOf[T](),
	}
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

// Set optional name(s)
func (b *BeanDefinitionImpl[T]) Name(names ...string) *BeanDefinitionImpl[T] {
	b.names = names
	return b
}

// Mark this bean as primary
func (b *BeanDefinitionImpl[T]) Primary() *BeanDefinitionImpl[T] {
	b.primary = true
	return b
}

// Profile binding
func (b *BeanDefinitionImpl[T]) Profile(profileExpr ...string) *BeanDefinitionImpl[T] {
	b.profiles = profileExpr
	return b
}

// Set the factory method reference or anonymous function with actual implementation
func (b *BeanDefinitionImpl[T]) Factory(f func() T) *BeanDefinitionImpl[T] {
	b.factoryMethod = f
	return b
}

// It is safe to use injected beans at this point
func (b *BeanDefinitionImpl[T]) PostConstruct(f func(T)) *BeanDefinitionImpl[T] {
	b.postConstructMethod = f
	return b
}

// Clean-up resources before shutdown. Not called on prototype beans.
func (b *BeanDefinitionImpl[T]) PreDestroy(f func(T)) *BeanDefinitionImpl[T] {
	lang.AssertState(b.scope != Prototype, "PreDestroy cannot be used for Prototype scope beans")
	b.preDestroyMethod = f
	return b
}

// Register the bean within the context
func (b *BeanDefinitionImpl[T]) Register() {
	lang.AssertState(b.factoryMethod != nil, "Bean factory method must be provided")
	applicationContextInstance().Register(b)
}

// Implements BeanDefinition
func (b *BeanDefinitionImpl[T]) getType() reflect.Type {
	return b.t
}

func (b *BeanDefinitionImpl[T]) getNames() []string {
	return b.names
}

func (b *BeanDefinitionImpl[T]) isPrimary() bool {
	return b.primary
}

func (b *BeanDefinitionImpl[T]) getScope() Scope {
	return b.scope
}

func (b *BeanDefinitionImpl[T]) getProfiles() []string {
	return b.profiles
}

func (b *BeanDefinitionImpl[T]) instantiate() any {
	b.instance = b.factoryMethod()
	b.postConstruct()
	return b.instance
}

func (b *BeanDefinitionImpl[T]) postConstruct() {
	if b.postConstructMethod != nil {
		b.postConstructMethod(b.instance.(T))
	}
}

func (b *BeanDefinitionImpl[T]) preDestroyEligible() bool {
	return b.preDestroyMethod != nil
}

func (b *BeanDefinitionImpl[T]) preDestroy() {
	defer func() {
		if err := recover(); err != nil {
			slog.Error(fmt.Sprintf("Could not destroy bean %v\n%v\n%s", b, err, debug.Stack()))
		}
	}()
	if b.instance != nil {
		b.preDestroyMethod(b.instance.(T))
	}
}

func (b *BeanDefinitionImpl[T]) getInstance() any {
	return b.instance
}

func (b *BeanDefinitionImpl[T]) getMutex() *sync.Mutex {
	return &b.mutex
}

// Implements String
func (b *BeanDefinitionImpl[T]) String() string {
	return fmt.Sprintf("%s[%s%s%s]", b.t, lang.If(b.scope == Singleton, "singleton", "prototype"), lang.If(b.primary, " primary", ""), lang.If(len(b.names) > 0, " "+strings.Join(b.names, ", "), ""))
}
