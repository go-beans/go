package ioc

import (
	"fmt"
	"log/slog"
	"path"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/env"
	"github.com/go-jang/go/lang"
	refl "github.com/go-jang/go/lang/reflect"
	"github.com/go-jang/go/util/concurrent"
)

type Scope int
type Profile string
type BeanInitializationState int

const (
	Singleton Scope = iota
	Prototype
)

const (
	BeanNotInitialized BeanInitializationState = iota
	BeanInitializing
	BeanInitialized
)

var lifecycleType = lang.TypeOf[Lifecycle]()
var phasedType = lang.TypeOf[Phased]()
var applicationRunnerType = lang.TypeOf[ApplicationRunner]()
var orderedType = lang.TypeOf[Ordered]()

var registered = make([]BeanDefinition, 0)
var registeredMu sync.Mutex

type BeanDefinition interface {
	getScope() Scope
	getType() reflect.Type
	getNames() []string
	isPrimary() bool
	isLazy() bool
	isLifecycleBean() bool
	isPhased() bool
	isApplicationRunner() bool
	isBeanPostProcessor() bool
	isOrdered() bool
	getDependsOn() []string
	getPhase() *int
	getOrder() *int
	getProfiles() []string
	getPropertyCondition() *PropertyCondition
	getExpressionCondition() *ExpressionCondition
	getBeanCondition() *BeanCondition
	getMissingBeanCondition() *MissingBeanCondition
	instantiate() any
	injectDependencies()
	reinjectDependencies()
	initialize()
	getInstance() any
	setInstance(instance any)
	getOriginalInstance() any
	getInstances() []any
	getInitializationState() BeanInitializationState
	compareAndSwapInitializationState(old, new BeanInitializationState) bool
	setInitializationState(state BeanInitializationState)
	preDestroyEligible() bool
	preDestroy()
	getMutex() *sync.Mutex
	getEventListenerMethods(eventType reflect.Type) []eventListenerMethod
	reset()
	String() string
}

type BeanDefinitionImpl[T any] struct {
	scope                Scope
	t                    reflect.Type
	names                []string
	primary              bool
	lazy                 bool
	dependsOn            []string
	phase                *int
	order                *int
	profiles             []string
	propertyCondition    *PropertyCondition
	expressionCondition  *ExpressionCondition
	beanCondition        *BeanCondition
	missingBeanCondition *MissingBeanCondition
	factoryMethod        func() T
	postConstructMethod  func(T)
	preDestroyMethod     func(T)
	instance             any
	originalInstance     any
	instances            []any
	initializationState  atomic.Int32
	mutex                sync.Mutex
	eventListenerMethods []eventListenerMethod
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
		panic(err.NewIllegalArgumentException(fmt.Sprintf("%s scope not supported", scope)))
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

// Order for slices and ApplicationRunner beans. Default: math.MaxInt
//
// lower order - executes first
//
// higher order - executes later
func (this *BeanDefinitionImpl[T]) Order(order int) *BeanDefinitionImpl[T] {
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

// ConditionalOnProperty registers the bean only when the specified configuration
// property exists or matches the expected value.
//
// When no expected value is provided, the condition also matches if any nested
// property exists. This allows beans configured through env.ConfigurationProperties()
// to be registered even when the configuration prefix has no direct value.
func (this *BeanDefinitionImpl[T]) ConditionalOnProperty(name string, value ...string) *BeanDefinitionImpl[T] {
	lang.Assert(this.propertyCondition == nil, "ConditionalOnProperty is defined twice")

	this.propertyCondition = NewPropertyCondition(name, value...)
	return this
}

// ConditionalOnExpression registers the bean only when the specified expression
// evaluates to true.
func (this *BeanDefinitionImpl[T]) ConditionalOnExpression(expression string) *BeanDefinitionImpl[T] {
	lang.Assert(expression != "", "Expression must not be empty")
	lang.Assert(this.expressionCondition == nil, "ConditionalOnExpression is defined twice")

	this.expressionCondition = NewExpressionCondition(expression)
	return this
}

// ConditionalOnBean registers the bean only when another bean of the specified
// type is selected for registration.
//
// An optional name restricts the condition to a bean matching both the type
// and the name.
func (this *BeanDefinitionImpl[T]) ConditionalOnBean(t reflect.Type, name ...string) *BeanDefinitionImpl[T] {
	lang.Assert(this.beanCondition == nil, "ConditionalOnBean is defined twice")

	this.beanCondition = NewBeanCondition(t, name...)
	return this
}

// ConditionalOnMissingBean registers the bean only when no bean of the specified
// type has been selected for registration.
//
// An optional name restricts the condition to beans matching both the type
// and the name. The condition takes into account registration order.
func (this *BeanDefinitionImpl[T]) ConditionalOnMissingBean(t reflect.Type, name ...string) *BeanDefinitionImpl[T] {
	lang.Assert(this.missingBeanCondition == nil, "ConditionalOnMissingBean is defined twice")

	this.missingBeanCondition = NewMissingBeanCondition(t, name...)
	return this
}

func (this *BeanDefinitionImpl[T]) EventListener(method any) *BeanDefinitionImpl[T] {
	methodValue := reflect.ValueOf(method)
	methodType := methodValue.Type()

	lang.Assert(methodType.Kind() == reflect.Func, "EventListener must be a method reference")
	lang.Assert(methodType.NumIn() == 2, "EventListener method must have receiver and one event argument")
	lang.Assert(methodType.NumOut() == 0, "EventListener method must not return values")

	receiverType := methodType.In(0)
	eventType := methodType.In(1)

	lang.Assert(this.t.AssignableTo(receiverType), "EventListener receiver %s does not match bean type %s", receiverType, this.t)

	this.eventListenerMethods = append(this.eventListenerMethods, eventListenerMethod{
		eventType: eventType,
		method:    methodValue,
	})

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

// Register the bean
func (this *BeanDefinitionImpl[T]) Register() {
	lang.Assert(this.factoryMethod != nil, "Bean factory method must be provided")
	register(this)
}

func (this *BeanDefinitionImpl[T]) getScope() Scope {
	return this.scope
}

// Implements BeanDefinition
func (this *BeanDefinitionImpl[T]) getType() reflect.Type {
	return this.t
}

func (this *BeanDefinitionImpl[T]) getNames() []string {
	if len(this.names) > 0 {
		return this.names
	}
	t := this.getType()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Name() == "" {
		return []string{t.String()}
	}
	pkg := t.PkgPath()
	if pkg == "" {
		return []string{t.Name()}
	}
	return []string{path.Base(pkg) + "." + t.Name()}
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

func (this *BeanDefinitionImpl[T]) isBeanPostProcessor() bool {
	return this.t.Implements(reflect.TypeFor[BeanPostProcessor]())
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

func (this *BeanDefinitionImpl[T]) getPropertyCondition() *PropertyCondition {
	return this.propertyCondition
}

func (this *BeanDefinitionImpl[T]) getExpressionCondition() *ExpressionCondition {
	return this.expressionCondition
}

func (this *BeanDefinitionImpl[T]) getBeanCondition() *BeanCondition {
	return this.beanCondition
}

func (this *BeanDefinitionImpl[T]) getMissingBeanCondition() *MissingBeanCondition {
	return this.missingBeanCondition
}

func (this *BeanDefinitionImpl[T]) instantiate() any {
	instance := this.factoryMethod()
	this.originalInstance = instance
	this.instance = instance
	this.instances = []any{instance}
	var obj any = instance
	if bean, ok := obj.(BeanNameAware); ok && len(this.names) > 0 {
		bean.SetBeanName(this.names[0])
	}
	if bean, ok := obj.(ApplicationContextAware); ok {
		bean.SetApplicationContext(applicationContextInstance())
	}
	return instance
}

func (this *BeanDefinitionImpl[T]) injectDependencies() {
	value := reflect.ValueOf(this.instance)
	if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Struct {
		env.BindPropertiesAny(this.instance)
		injectBeansAny(this.instance)
	}
}

func (this *BeanDefinitionImpl[T]) reinjectDependencies() {
	for _, instance := range this.instances {
		value := reflect.ValueOf(instance)
		if value.Kind() == reflect.Pointer && !value.IsNil() && value.Elem().Kind() == reflect.Struct {
			this.reinjectBeansAny(instance)
		}
	}
}

func (this *BeanDefinitionImpl[T]) reinjectBeansAny(target any) {
	refl.ForEachTaggedField(target, InjectTag, func(field refl.Field) {
		current := field.Value
		if current.Kind() == reflect.Interface {
			if current.IsNil() {
				return
			}
			current = current.Elem()
		}
		if current.Kind() != reflect.Pointer || current.IsNil() {
			return
		}
		for _, dependency := range applicationContextInstance().instantiated {
			for _, instance := range dependency.getInstances() {
				candidate := reflect.ValueOf(instance)
				if candidate.Kind() != reflect.Pointer || candidate.IsNil() {
					continue
				}
				if current.Type() != candidate.Type() || current.Pointer() != candidate.Pointer() {
					continue
				}
				replacement := reflect.ValueOf(dependency.getInstance())
				if !replacement.IsValid() {
					return
				}
				if replacement.Kind() == reflect.Pointer && replacement.Type() == current.Type() && replacement.Pointer() == current.Pointer() {
					return
				}
				lang.Assert(replacement.Type().AssignableTo(field.Type), "Cannot reinject field '%s': final bean %s is not assignable to %s", field.Field.Name, replacement.Type(), field.Type)
				field.Value.Set(replacement)
				return
			}
		}
	})
}

func (this *BeanDefinitionImpl[T]) initialize() {
	if this.postConstructMethod != nil {
		this.postConstructMethod(this.originalInstance.(T))
	}
	if bean, ok := this.originalInstance.(InitializingBean); ok {
		bean.AfterPropertiesSet()
	}
}

func (this *BeanDefinitionImpl[T]) preDestroyEligible() bool {
	var obj any = this.originalInstance
	_, isDisposable := obj.(DisposableBean)
	return this.scope == Singleton && (this.preDestroyMethod != nil || isDisposable)
}

func (this *BeanDefinitionImpl[T]) preDestroy() {
	defer err.Recover(func(e any) {
		slog.Error(fmt.Sprintf("Could not destroy bean %v. %s", this, err.PrintStackTrace(e)))
	})
	if this.preDestroyMethod != nil {
		this.preDestroyMethod(this.originalInstance.(T))
	}
	var obj any = this.originalInstance
	if bean, ok := obj.(DisposableBean); ok {
		bean.Destroy()
	}
}

func (this *BeanDefinitionImpl[T]) getInstance() any {
	return this.instance
}

func (this *BeanDefinitionImpl[T]) setInstance(instance any) {
	this.instance = instance
	this.instances = append(this.instances, instance)
}

func (this *BeanDefinitionImpl[T]) getOriginalInstance() any {
	return this.originalInstance
}

func (this *BeanDefinitionImpl[T]) getInstances() []any {
	return this.instances
}

func (this *BeanDefinitionImpl[T]) getInitializationState() BeanInitializationState {
	return BeanInitializationState(this.initializationState.Load())
}

func (this *BeanDefinitionImpl[T]) compareAndSwapInitializationState(old, new BeanInitializationState) bool {
	return this.initializationState.CompareAndSwap(int32(old), int32(new))
}

func (this *BeanDefinitionImpl[T]) setInitializationState(state BeanInitializationState) {
	this.initializationState.Store(int32(state))
}

func (this *BeanDefinitionImpl[T]) getMutex() *sync.Mutex {
	return &this.mutex
}

func (this *BeanDefinitionImpl[T]) getEventListenerMethods(eventType reflect.Type) []eventListenerMethod {
	methods := make([]eventListenerMethod, 0)
	for _, listener := range this.eventListenerMethods {
		if eventType.AssignableTo(listener.eventType) {
			methods = append(methods, listener)
		}
	}
	return methods
}

func (this *BeanDefinitionImpl[T]) reset() {
	this.instance = nil
	this.originalInstance = nil
	this.instances = nil
	this.initializationState.Store(int32(BeanNotInitialized))
}

// Implements String
func (this *BeanDefinitionImpl[T]) String() string {
	return fmt.Sprintf("%s [%s%s%s%s%s%s%s]", this.t,
		lang.If(this.scope == Singleton, "singleton", "prototype"),
		lang.If(len(this.names) > 0, " "+strings.Join(this.names, ", "), ""),
		lang.If(this.primary, " primary", ""),
		lang.If(this.lazy, " lazy", ""),
		lang.If(this.isLifecycleBean(), " Lifecycle", ""),
		lang.If(this.isApplicationRunner(), " ApplicationRunner", ""),
		lang.If(this.isBeanPostProcessor(), " BeanPostProcessor", ""))
}

type eventListenerMethod struct {
	eventType reflect.Type
	method    reflect.Value
}

func (this eventListenerMethod) invoke(bean any, event reflect.Value) {
	this.method.Call([]reflect.Value{reflect.ValueOf(bean), event})
}

func register(bean BeanDefinition) {
	concurrent.Synchronized(&registeredMu, func() {
		context := applicationContext.Load()
		lang.Assert(context == nil || !context.refreshing.Load(), "Cannot register bean after ApplicationContext refresh has started")

		registered = append(registered, bean)
	})
}

func resetBeanDefinitions() {
	concurrent.Synchronized(&registeredMu, func() {
		for _, bean := range registered {
			bean.reset()
		}
	})
}
