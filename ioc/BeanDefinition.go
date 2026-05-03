package ioc

import (
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/lang"
)

type Scope int
type Profile string

const (
	Singleton Scope = iota
	Prototype
)

var lifecycleType = lang.TypeOf[Lifecycle]()
var phasedType = lang.TypeOf[Phased]()
var applicationRunnerType = lang.TypeOf[ApplicationRunner]()
var orderedType = lang.TypeOf[Ordered]()

type BeanDefinition interface {
	getScope() Scope
	getType() reflect.Type
	getNames() []string
	isPrimary() bool
	isLazy() bool
	isLifecycleBean() bool
	isPhased() bool
	isApplicationRunner() bool
	isOrdered() bool
	getDependsOn() []string
	getPhase() *int
	getOrder() *int
	getProfiles() []string
	instantiate() any
	getInstance() any
	preDestroyEligible() bool
	preDestroy()
	getMutex() *sync.Mutex
	String() string
}

type BeanDefinitionImpl[T any] struct {
	scope               Scope
	t                   reflect.Type
	names               []string
	primary             bool
	lazy                bool
	dependsOn           []string
	phase               *int
	order               *int
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
	lang.Assert(this.names == nil, "Name is defined twice")
	this.names = names
	return this
}

// Mark this bean as primary
func (this *BeanDefinitionImpl[T]) Primary() *BeanDefinitionImpl[T] {
	lang.Assert(!this.primary, "Primary is defined twice")
	this.primary = true
	return this
}

// Mark this bean as lazy
func (this *BeanDefinitionImpl[T]) Lazy() *BeanDefinitionImpl[T] {
	lang.Assert(!this.lazy, "Lazy is defined twice")
	this.lazy = true
	return this
}

// Depends on beans initialization
func (this *BeanDefinitionImpl[T]) DependsOn(beans ...string) *BeanDefinitionImpl[T] {
	lang.Assert(this.dependsOn == nil, "DependsOn is defined twice")
	this.dependsOn = beans
	return this
}

// Phase for Lifecycle beans. Default: 0
//
// phase 0 - normal components
//
// negative phases - infrastructure
//
// positive phases - late-start services
func (this *BeanDefinitionImpl[T]) Phase(phase int) *BeanDefinitionImpl[T] {
	lang.Assert(this.isLifecycleBean(), "Phase may be applied only to Lifecycle bean")
	lang.Assert(this.phase == nil, "Phase is defined twice")
	this.phase = &phase
	return this
}

// Order for ApplicationRunner beans. Default: math.MaxInt
//
// lower order - executes first
//
// higher order - executes later
func (this *BeanDefinitionImpl[T]) Order(order int) *BeanDefinitionImpl[T] {
	lang.Assert(this.isApplicationRunner(), "Order may be applied only to ApplicationRunner")
	lang.Assert(this.order == nil, "Order is defined twice")
	this.order = &order
	return this
}

// Profile binding
func (this *BeanDefinitionImpl[T]) Profile(profileExpr ...string) *BeanDefinitionImpl[T] {
	lang.Assert(this.profiles == nil, "Profile is defined twice")
	this.profiles = profileExpr
	return this
}

// Set the factory method reference or anonymous function with actual implementation
func (this *BeanDefinitionImpl[T]) Factory(f func() T) *BeanDefinitionImpl[T] {
	lang.Assert(this.factoryMethod == nil, "Factory is defined twice")
	this.factoryMethod = f
	return this
}

// It is safe to use injected beans at this point
func (this *BeanDefinitionImpl[T]) PostConstruct(f func(T)) *BeanDefinitionImpl[T] {
	lang.Assert(this.postConstructMethod == nil, "PostConstruct is defined twice")
	this.postConstructMethod = f
	return this
}

// Clean-up resources before shutdown. Not called on prototype beans.
func (this *BeanDefinitionImpl[T]) PreDestroy(f func(T)) *BeanDefinitionImpl[T] {
	lang.Assert(this.preDestroyMethod == nil, "PreDestroy is defined twice")
	lang.Assert(this.scope != Prototype, "PreDestroy cannot be used for Prototype scope beans")
	this.preDestroyMethod = f
	return this
}

// Register the bean within the context
func (this *BeanDefinitionImpl[T]) Register() {
	lang.Assert(this.factoryMethod != nil, "Bean factory method must be provided")
	applicationContextInstance().Register(this)
}

func (this *BeanDefinitionImpl[T]) getScope() Scope {
	return this.scope
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

func (this *BeanDefinitionImpl[T]) isLifecycleBean() bool {
	return this.getType().Implements(lifecycleType)
}

func (this *BeanDefinitionImpl[T]) isPhased() bool {
	return this.getType().Implements(phasedType)
}

func (this *BeanDefinitionImpl[T]) isApplicationRunner() bool {
	return this.getType().Implements(applicationRunnerType)
}

func (this *BeanDefinitionImpl[T]) isOrdered() bool {
	return this.getType().Implements(orderedType)
}

func (this *BeanDefinitionImpl[T]) getDependsOn() []string {
	return this.dependsOn
}

func (this *BeanDefinitionImpl[T]) getPhase() *int {
	return this.phase
}

func (this *BeanDefinitionImpl[T]) getOrder() *int {
	return this.order
}

func (this *BeanDefinitionImpl[T]) getProfiles() []string {
	return this.profiles
}

func (this *BeanDefinitionImpl[T]) instantiate() any {
	instance := this.factoryMethod()
	this.instance = instance
	var obj any = instance
	if bean, ok := obj.(BeanNameAware); ok && len(this.names) > 0 {
		bean.SetBeanName(this.names[0])
	}
	if bean, ok := obj.(EnvironmentAware); ok {
		bean.SetEnvironment(env.Instance())
	}
	if reflect.ValueOf(instance).Kind() == reflect.Pointer {
		env.BindPropertiesAny(instance)
		InjectBeansAny(instance)
	}
	if this.postConstructMethod != nil {
		this.postConstructMethod(instance)
	}
	if bean, ok := obj.(InitializingBean); ok {
		bean.AfterPropertiesSet()
	}
	return instance
}

func (this *BeanDefinitionImpl[T]) preDestroyEligible() bool {
	var obj any = this.instance
	_, isDisposable := obj.(DisposableBean)
	return this.scope == Singleton && (this.preDestroyMethod != nil || isDisposable)
}

func (this *BeanDefinitionImpl[T]) preDestroy() {
	defer err.Recover(func(e any) {
		slog.Error(fmt.Sprintf("Could not destroy bean %v. %s", this, err.PrintStackTrace(e)))
	})
	if this.preDestroyMethod != nil {
		this.preDestroyMethod(this.instance.(T))
	}
	var obj any = this.instance
	if bean, ok := obj.(DisposableBean); ok {
		bean.Destroy()
	}
}

func (this *BeanDefinitionImpl[T]) getInstance() any {
	return this.instance
}

func (this *BeanDefinitionImpl[T]) getMutex() *sync.Mutex {
	return &this.mutex
}

// Implements String
func (this *BeanDefinitionImpl[T]) String() string {
	return fmt.Sprintf("%s[%s%s%s%s%s]", this.t,
		lang.If(this.scope == Singleton, "singleton", "prototype"),
		lang.If(this.primary, " primary", ""),
		lang.If(this.isLifecycleBean(), " Lifecycle", ""),
		lang.If(this.isApplicationRunner(), " ApplicationRunner", ""),
		lang.If(len(this.names) > 0, " "+strings.Join(this.names, ", "), ""))
}
