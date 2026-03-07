package ioc

import (
	"fmt"
	"log/slog"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/go-external-config/go/lang"
	"github.com/go-external-config/go/util/err"
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
	isLazy() bool
	getDependsOn() []string
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
	lazy                bool
	dependsOn           []string
	profiles            []string
	factoryMethod       func() T
	postConstructMethod func(T)
	preDestroyMethod    func(T)
	instance            any
	mutex               sync.Mutex
}

func newBeanDefinition[T any]() *BeanDefinitionImpl[T] {
	return &BeanDefinitionImpl[T]{
		t:         lang.TypeOf[T](),
		names:     make([]string, 0),
		dependsOn: make([]string, 0),
		profiles:  make([]string, 0),
	}
}

// Set optional scope
func (this *BeanDefinitionImpl[T]) Scope(scope string) *BeanDefinitionImpl[T] {
	switch scope {
	case "singleton":
		this.scope = Singleton
	case "prototype":
		this.scope = Prototype
	default:
		panic(fmt.Sprintf("%s scope not supported", scope))
	}
	return this
}

// Set optional name(s)
func (this *BeanDefinitionImpl[T]) Name(names ...string) *BeanDefinitionImpl[T] {
	lang.AssertState(len(this.names) == 0, "Name is defined twice")
	this.names = names
	return this
}

// Mark this bean as primary
func (this *BeanDefinitionImpl[T]) Primary() *BeanDefinitionImpl[T] {
	lang.AssertState(!this.primary, "Primary is defined twice")
	this.primary = true
	return this
}

// Mark this bean as lazy
func (this *BeanDefinitionImpl[T]) Lazy() *BeanDefinitionImpl[T] {
	lang.AssertState(!this.lazy, "Lazy is defined twice")
	this.lazy = true
	return this
}

// Depends on beans.
func (this *BeanDefinitionImpl[T]) DependsOn(beans ...string) *BeanDefinitionImpl[T] {
	lang.AssertState(len(this.dependsOn) == 0, "DependsOn is defined twice")
	this.dependsOn = beans
	return this
}

// Profile binding
func (this *BeanDefinitionImpl[T]) Profile(profileExpr ...string) *BeanDefinitionImpl[T] {
	lang.AssertState(len(this.profiles) == 0, "Profile is defined twice")
	this.profiles = profileExpr
	return this
}

// Set the factory method reference or anonymous function with actual implementation
func (this *BeanDefinitionImpl[T]) Factory(f func() T) *BeanDefinitionImpl[T] {
	lang.AssertState(this.factoryMethod == nil, "Factory is defined twice")
	this.factoryMethod = f
	return this
}

// It is safe to use injected beans at this point
func (this *BeanDefinitionImpl[T]) PostConstruct(f func(T)) *BeanDefinitionImpl[T] {
	lang.AssertState(this.postConstructMethod == nil, "PostConstruct is defined twice")
	this.postConstructMethod = f
	return this
}

// Clean-up resources before shutdown. Not called on prototype beans.
func (this *BeanDefinitionImpl[T]) PreDestroy(f func(T)) *BeanDefinitionImpl[T] {
	lang.AssertState(this.preDestroyMethod == nil, "PreDestroy is defined twice")
	lang.AssertState(this.scope != Prototype, "PreDestroy cannot be used for Prototype scope beans")
	this.preDestroyMethod = f
	return this
}

// Register the bean within the context
func (this *BeanDefinitionImpl[T]) Register() {
	lang.AssertState(this.factoryMethod != nil, "Bean factory method must be provided")
	applicationContextInstance().Register(this)
}

// Implements BeanDefinition
func (this *BeanDefinitionImpl[T]) getType() reflect.Type {
	return this.t
}

func (this *BeanDefinitionImpl[T]) getNames() []string {
	return this.names
}

func (this *BeanDefinitionImpl[T]) isPrimary() bool {
	return this.primary
}

func (this *BeanDefinitionImpl[T]) isLazy() bool {
	return this.lazy
}

func (this *BeanDefinitionImpl[T]) getDependsOn() []string {
	return this.dependsOn
}

func (this *BeanDefinitionImpl[T]) getScope() Scope {
	return this.scope
}

func (this *BeanDefinitionImpl[T]) getProfiles() []string {
	return this.profiles
}

func (this *BeanDefinitionImpl[T]) instantiate() any {
	this.instance = this.factoryMethod()
	this.postConstruct()
	return this.instance
}

func (this *BeanDefinitionImpl[T]) postConstruct() {
	if this.postConstructMethod != nil {
		this.postConstructMethod(this.instance.(T))
	}
}

func (this *BeanDefinitionImpl[T]) preDestroyEligible() bool {
	return this.instance != nil && this.preDestroyMethod != nil && this.scope == Singleton
}

func (this *BeanDefinitionImpl[T]) preDestroy() {
	defer err.Recover(func(err any) {
		slog.Error(fmt.Sprintf("Could not destroy bean %v\n%v\n%s", this, err, debug.Stack()))
	})
	this.preDestroyMethod(this.instance.(T))
}

func (this *BeanDefinitionImpl[T]) getInstance() any {
	return this.instance
}

func (this *BeanDefinitionImpl[T]) getMutex() *sync.Mutex {
	return &this.mutex
}

// Implements String
func (this *BeanDefinitionImpl[T]) String() string {
	return fmt.Sprintf("%s[%s%s%s]", this.t, lang.If(this.scope == Singleton, "singleton", "prototype"), lang.If(this.primary, " primary", ""), lang.If(len(this.names) > 0, " "+strings.Join(this.names, ", "), ""))
}
