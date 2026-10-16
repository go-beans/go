package ioc

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-errr/go/err"
	"github.com/go-external-config/go/env"
	"github.com/go-jang/go/lang"
	"github.com/go-jang/go/util/collections"
	"github.com/go-jang/go/util/concurrent"
)

var applicationContext atomic.Pointer[ApplicationContext]
var applicationContextMu sync.Mutex

type ApplicationContext struct {
	context             context.Context
	cancel              context.CancelFunc
	resolved            []BeanDefinition
	instantiated        []BeanDefinition
	instantiatedMu      sync.RWMutex
	started             []BeanDefinition
	beans               map[reflect.Type][]BeanDefinition
	named               map[string]BeanDefinition
	postProcessors      []BeanDefinition
	initializationReady bool
	eventListenersCache *concurrent.HashMap[reflect.Type, []eventListener]
	refreshing          atomic.Bool
	definitionsResolved atomic.Bool
	refreshed           atomic.Bool
	startTime           time.Time
	servicesCount       atomic.Int32
	closing             atomic.Bool
	exiting             atomic.Bool
}

func applicationContextInstance() *ApplicationContext {
	if applicationContext.Load() == nil {
		concurrent.Synchronized(&applicationContextMu, func() {
			if applicationContext.Load() == nil {
				applicationContext.Store(newApplicationContext())
			}
		})
	}
	return applicationContext.Load()
}

func newApplicationContext() *ApplicationContext {
	slog.Info(fmt.Sprintf("Starting with PID %d", os.Getpid()))
	context, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	return &ApplicationContext{
		context:             context,
		cancel:              cancel,
		resolved:            make([]BeanDefinition, 0),
		instantiated:        make([]BeanDefinition, 0),
		beans:               make(map[reflect.Type][]BeanDefinition),
		named:               make(map[string]BeanDefinition),
		eventListenersCache: concurrent.NewHashMap[reflect.Type, []eventListener](),
		startTime:           time.Now(),
	}
}

func (this *ApplicationContext) bean(inject *InjectQualifier[any]) any {
	defer err.Catch(func(e any) {
		if inject.fieldName == "" {
			panic(e)
		} else {
			panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Cannot inject dependency into field '%s' of type %s", inject.fieldName, inject.t), e))
		}
	})

	lang.Assert(this.definitionsResolved.Load(), "ApplicationContext has not been refreshed yet")

	if len(inject.name) > 0 {
		bean, ok := this.named[inject.name]
		if !ok && inject.optional {
			return nil
		}
		lang.Assert(ok, "No bean named '%s' found", inject.name)
		return this.beanInstance(bean)

	} else if inject.t.Kind() == reflect.Slice {
		elemType := inject.t.Elem()
		orderedBeans := this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
			return this.eligible(bean.getType(), elemType)
		})
		result := reflect.MakeSlice(inject.t, 0, 0)
		for _, bean := range orderedBeans {
			value := reflect.ValueOf(bean.getInstance())
			lang.Assert(value.Type().AssignableTo(elemType), "Bean %s is not assignable to %s", value.Type(), elemType)
			result = reflect.Append(result, value)
		}
		return result.Interface()

	} else {
		var candidates []BeanDefinition
		var primaryCandidates []BeanDefinition

		for t, beans := range this.beans {
			if this.eligible(t, inject.t) {
				candidates = append(candidates, beans...)
				for _, bean := range beans {
					if bean.isPrimary() {
						primaryCandidates = append(primaryCandidates, bean)
					}
				}
			}
		}

		lang.Assert(len(primaryCandidates) <= 1, "Multiple primary beans of type %v found. Use name qualifier.\n%v", inject.t, primaryCandidates)
		if len(primaryCandidates) == 1 {
			return this.beanInstance(primaryCandidates[0])
		} else {
			if len(candidates) == 0 && inject.optional {
				return nil
			}
			lang.Assert(len(candidates) > 0, "No bean of type %v found", inject.t)
			lang.Assert(len(candidates) <= 1, "Multiple beans of type %v found. Use name qualifier or mark one of the beans primary.\n%v", inject.t, candidates)
			return this.beanInstance(candidates[0])
		}
	}
}

func (this *ApplicationContext) beanInstance(bean BeanDefinition) any {
	return this.beanInstanceWithPath(bean, []string{bean.getNames()[0]})
}

func (this *ApplicationContext) beanInstanceWithPath(bean BeanDefinition, path []string) (instance any) {
	err.Try(func() {
		this.validateDependencies(bean, path)

		if bean.getScope() == Singleton {
			if bean.getInstance() == nil {
				concurrent.Synchronized(bean.getMutex(), func() {
					if bean.getInstance() == nil {
						this.servicesCount.Add(1)
						bean.instantiate()
						this.instantiateDependencies(bean, path)
						bean.injectDependencies()
						this.registerInstantiatedBean(bean)
					}
				})
			}
			instance = bean.getInstance()
		} else {
			concurrent.Synchronized(bean.getMutex(), func() {
				instance = bean.instantiate()
				this.instantiateDependencies(bean, path)
				bean.injectDependencies()
				bean.initialize()
			})
		}
	}, func(e any) {
		panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error creating bean %v", bean), e))
	})

	if bean.getScope() == Singleton && this.initializationReady && bean.getInitializationState() == BeanNotInitialized {
		this.initializeBean(bean)
		this.reinjectDependencies()
	}

	return instance
}

func (this *ApplicationContext) validateDependencies(bean BeanDefinition, path []string) {
	for _, name := range bean.getDependsOn() {
		for _, visited := range path {
			lang.Assert(visited != name, "Circular DependsOn dependency: %s", strings.Join(append(path, name), " -> "))
		}
	}
}

func (this *ApplicationContext) instantiateDependencies(bean BeanDefinition, path []string) {
	for _, name := range bean.getDependsOn() {
		dependency, ok := this.named[name]
		lang.Assert(ok, "No dependency bean named '%s' found", name)
		this.beanInstanceWithPath(dependency, append(path, name))
	}
}

func (this *ApplicationContext) registerInstantiatedBean(bean BeanDefinition) {
	this.instantiatedMu.Lock()
	defer this.instantiatedMu.Unlock()

	this.instantiated = append(this.instantiated, bean)
	this.eventListenersCache.Clear()
}

func (this *ApplicationContext) instantiatedSnapshot() []BeanDefinition {
	this.instantiatedMu.RLock()
	defer this.instantiatedMu.RUnlock()

	return this.instantiated
}

func (this *ApplicationContext) eligible(registered, requested reflect.Type) bool {
	return registered.AssignableTo(requested)
}

func (this *ApplicationContext) refresh() {
	defer err.Recover(func(e any) {
		this.exit1(e, "Context refresh failed.")
	})
	this.doRefresh()
}

func (this *ApplicationContext) doRefresh() {
	threshold := time.Now()
	concurrent.Synchronized(&registeredMu, func() {
		lang.Assert(this.refreshing.CompareAndSwap(false, true), "ApplicationContext has already been refreshed")
		this.resolveBeanDefinitions()
	})

	this.initializeBeans()
	this.startLifecycleBeans()
	this.refreshed.Store(true)

	slog.Info(fmt.Sprintf("Context refreshed in %v", time.Since(threshold)))
	this.PublishEvent(NewContextRefreshedEvent(this))
}

func (this *ApplicationContext) resolveBeanDefinitions() {
	candidates := make([]BeanDefinition, 0)
	selected := make(map[BeanDefinition]bool)

	for _, bean := range registered {
		if !env.MatchesProfiles(bean.getProfiles()...) {
			continue
		} else if condition := bean.getPropertyCondition(); condition != nil && !condition.Matches() {
			continue
		} else if condition := bean.getExpressionCondition(); condition != nil && !condition.Matches() {
			continue
		}
		candidates = append(candidates, bean)
		if bean.getBeanCondition() == nil && bean.getMissingBeanCondition() == nil {
			selected[bean] = true
		}
	}

	for {
		size := len(selected)
		selected = this.resolveConditionalBeanDefinitions(candidates, selected)
		selected = this.resolveConditionalMissingBeanDefinitions(candidates, selected)
		if len(selected) == size {
			break
		}
	}

	for _, bean := range candidates {
		if !selected[bean] {
			continue
		}
		for _, name := range bean.getNames() {
			_, ok := this.named[name]
			lang.Assert(!ok, "Bean with name '%s' already registered", name)
			this.named[name] = bean
		}
		this.beans[bean.getType()] = append(this.beans[bean.getType()], bean)
		this.resolved = append(this.resolved, bean)
		slog.Debug(fmt.Sprintf("Registered %v", bean))
	}
	this.definitionsResolved.Store(true)
}

func (this *ApplicationContext) resolveConditionalBeanDefinitions(candidates []BeanDefinition, selected map[BeanDefinition]bool) map[BeanDefinition]bool {
	for {
		available := make([]BeanDefinition, 0, len(selected))
		for _, bean := range candidates {
			if selected[bean] {
				available = append(available, bean)
			}
		}

		size := len(selected)
		for _, bean := range candidates {
			if selected[bean] || bean.getMissingBeanCondition() != nil {
				// Definitions with MissingBean conditions are resolved later to preserve logical AND semantics.
				continue
			} else if condition := bean.getBeanCondition(); condition != nil && condition.Matches(available) {
				selected[bean] = true
			}
		}

		if len(selected) == size {
			break
		}
	}
	return selected
}

func (this *ApplicationContext) resolveConditionalMissingBeanDefinitions(candidates []BeanDefinition, selected map[BeanDefinition]bool) map[BeanDefinition]bool {
	available := make([]BeanDefinition, 0, len(selected))
	for _, bean := range candidates {
		if selected[bean] {
			available = append(available, bean)
		}
	}

	for _, bean := range candidates {
		if selected[bean] {
			continue
		}
		condition := bean.getMissingBeanCondition()
		if condition == nil {
			continue
		} else if beanCondition := bean.getBeanCondition(); beanCondition != nil && !beanCondition.Matches(available) {
			continue
		} else if condition.Matches(available) {
			selected[bean] = true
			available = append(available, bean)
		}
	}
	return selected
}

func (this *ApplicationContext) initializeBeans() {
	nonLazySingletons := this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
		return bean.getScope() == Singleton && !bean.isLazy() && !bean.isBeanPostProcessor()
	})

	this.initializePostProcessors()

	for _, bean := range nonLazySingletons {
		this.initializeBean(bean)
	}
	for i := 0; i < len(this.instantiatedSnapshot()); i++ {
		bean := this.instantiatedSnapshot()[i]
		if bean.isLazy() && !bean.isBeanPostProcessor() {
			this.initializeBean(bean)
		}
	}
	this.reinjectDependencies()
	this.initializationReady = true
}

func (this *ApplicationContext) initializePostProcessors() {
	this.postProcessors = this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
		if !bean.isBeanPostProcessor() {
			return false
		}
		lang.Assert(bean.getScope() == Singleton, "PostProcessor must be a singleton: %s", bean)
		return true
	})

	for _, processor := range this.postProcessors {
		err.Try(func() {
			processor.initialize()
			processor.setInitializationState(BeanInitialized)
		}, func(e any) {
			panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error initializing bean %v", processor), e))
		})
	}
}

func (this *ApplicationContext) initializeBean(bean BeanDefinition) {
	if !bean.compareAndSwapInitializationState(BeanNotInitialized, BeanInitializing) {
		return
	}
	defer err.Catch(func(e any) {
		panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error initializing bean %v", bean), e))
	})

	for _, name := range bean.getDependsOn() {
		dependency, ok := this.named[name]
		lang.Assert(ok, "No dependency bean named '%s' found", name)
		if dependency.getScope() == Singleton {
			this.beanInstance(dependency)
			this.initializeBean(dependency)
		}
	}

	this.postProcessBeforeInitialization(bean)
	bean.initialize()
	this.postProcessAfterInitialization(bean)

	bean.setInitializationState(BeanInitialized)
}

func (this *ApplicationContext) postProcessBeforeInitialization(bean BeanDefinition) {
	for _, processor := range this.postProcessors {
		current := bean.getInstance()
		replacement := processor.getInstance().(BeanPostProcessor).PostProcessBeforeInitialization(current, bean.getNames()[0])
		if replacement == nil {
			break
		}
		if reflect.TypeOf(current) == reflect.TypeOf(replacement) && reflect.TypeOf(current).Comparable() && current == replacement {
			continue
		}
		lang.Assert(reflect.TypeOf(replacement).AssignableTo(bean.getType()), "BeanPostProcessor %T returned incompatible replacement %T for bean '%s' of type %s", processor.getInstance(), replacement, bean.getNames()[0], bean.getType())
		bean.setInstance(replacement)
		bean.injectDependencies()
	}
}

func (this *ApplicationContext) postProcessAfterInitialization(bean BeanDefinition) {
	for _, processor := range this.postProcessors {
		current := bean.getInstance()
		replacement := processor.getInstance().(BeanPostProcessor).PostProcessAfterInitialization(current, bean.getNames()[0])
		if replacement == nil {
			break
		}
		if reflect.TypeOf(current) == reflect.TypeOf(replacement) && reflect.TypeOf(current).Comparable() && current == replacement {
			continue
		}
		lang.Assert(reflect.TypeOf(replacement).AssignableTo(bean.getType()), "BeanPostProcessor %T returned incompatible replacement %T for bean '%s' of type %s", processor.getInstance(), replacement, bean.getNames()[0], bean.getType())
		bean.setInstance(replacement)
		bean.injectDependencies()
	}
}

func (this *ApplicationContext) reinjectDependencies() {
	for _, bean := range this.instantiatedSnapshot() {
		bean.reinjectDependencies()
	}
}

func (this *ApplicationContext) startLifecycleBeans() {
	executor := concurrent.NewExecutor(runtime.NumCPU(), 0)
	defer executor.Shutdown()

	phaseToBeans := this.phaseToLifecycleBeans(this.resolved)
	sortedPhases := make([]int, 0, len(phaseToBeans))
	for phase := range phaseToBeans {
		sortedPhases = append(sortedPhases, phase)
	}
	sort.Ints(sortedPhases)

	var er error
	for _, phase := range sortedPhases {
		beans := phaseToBeans[phase]
		futures := make([]concurrent.Future[BeanDefinition], 0)
		for _, bean := range beans {
			futures = append(futures, concurrent.Submit(executor, func(_ context.Context) BeanDefinition {
				instance := this.beanInstance(bean)
				err.Try(func() {
					instance.(Lifecycle).Start()
				}, func(e any) {
					panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error starting bean %v", bean), e))
				})
				return bean
			}))
		}
		for _, future := range futures {
			bean, e := future.Result()
			if e == nil {
				this.started = append(this.started, bean)
			} else if er == nil {
				er = e
			}
		}
		if er != nil {
			panic(er)
		}
	}
}

func (this *ApplicationContext) phaseToLifecycleBeans(beans []BeanDefinition) map[int][]BeanDefinition {
	phaseToBeans := make(map[int][]BeanDefinition)
	this.foreachBeanDefinition(beans, func(bean BeanDefinition) bool {
		return bean.isLifecycleBean()
	}, func(bean BeanDefinition) {
		lang.Assert(bean.getScope() == Singleton, "Lifecycle bean must be a singleton: %s", bean)
		phase := math.MaxInt
		if bean.getPhase() != nil {
			phase = *bean.getPhase()
		} else if bean.isPhased() {
			phase = this.beanInstance(bean).(Phased).Phase()
		}
		beans, ok := phaseToBeans[phase]
		if !ok {
			beans = make([]BeanDefinition, 0)
		}
		beans = append(beans, bean)
		phaseToBeans[phase] = beans
	})
	return phaseToBeans
}

func (this *ApplicationContext) orderedBeanInstances(beans []BeanDefinition, filter func(b BeanDefinition) bool) []BeanDefinition {
	orderToBeans := make(map[int][]BeanDefinition)
	this.foreachBeanDefinition(beans, filter, func(bean BeanDefinition) {
		instance := this.beanInstance(bean)
		order := math.MaxInt
		if bean.getOrder() != nil {
			order = *bean.getOrder()
		} else if bean.isOrdered() {
			order = instance.(Ordered).Order()
		}
		beans, ok := orderToBeans[order]
		if !ok {
			beans = make([]BeanDefinition, 0)
		}
		beans = append(beans, bean)
		orderToBeans[order] = beans
	})

	sortedOrder := make([]int, 0, len(orderToBeans))
	for order := range orderToBeans {
		sortedOrder = append(sortedOrder, order)
	}
	sort.Ints(sortedOrder)

	orderedBeans := make([]BeanDefinition, 0)
	for _, order := range sortedOrder {
		orderedBeans = append(orderedBeans, orderToBeans[order]...)
	}
	return orderedBeans
}

func (this *ApplicationContext) run() {
	defer err.Recover(func(e any) {
		this.publishEvent(NewApplicationFailedEvent(e), true)
		this.exit1(e, "Context run failed.")
	})

	if !this.refreshed.Load() {
		this.doRefresh()
	}
	this.PublishEvent(NewApplicationStartedEvent())
	this.executeApplicationRunnerBeans()
	this.PublishEvent(NewApplicationReadyEvent(time.Since(this.startTime)))
}

func (this *ApplicationContext) executeApplicationRunnerBeans() {
	orderedBeans := this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
		return bean.isApplicationRunner()
	})
	for _, bean := range orderedBeans {
		instance := this.beanInstance(bean)
		err.Try(func() {
			instance.(ApplicationRunner).Run(os.Args)
		}, func(e any) {
			panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error running bean %v", bean), e))
		})
	}
}

func (this *ApplicationContext) close() {
	concurrent.Synchronized(&applicationContextMu, func() {
		if this.closing.CompareAndSwap(false, true) {
			threshold := time.Now()
			slog.Info(fmt.Sprintf("Closing context with %d running services", this.servicesCount.Load()))
			this.publishEvent(NewContextClosedEvent(), true)

			this.cancel()
			this.stopLifecycleBeans()
			this.destroyBeans()
			resetBeanDefinitions()

			slog.Info(fmt.Sprintf("Context closed in %v, uptime %v", time.Since(threshold), time.Since(this.startTime)))
			applicationContext.CompareAndSwap(this, nil)
		}
	})
}

func (this *ApplicationContext) exit(code int, format string, a ...any) {
	if !this.exiting.CompareAndSwap(false, true) {
		runtime.Goexit()
	}
	message := fmt.Sprintf(format, a...)
	if code == 0 {
		slog.Info(message)
	} else {
		slog.Error(err.PrintStackTrace(err.NewIllegalStateExceptionWith(message, nil, err.StackTrace(1))))
	}
	this.close()
	os.Exit(code)
}

func (this *ApplicationContext) exit1(e any, format string, a ...any) {
	if !this.exiting.CompareAndSwap(false, true) {
		runtime.Goexit()
	}
	message := fmt.Sprintf(format, a...)
	slog.Error(fmt.Sprintf("%s %s", message, err.PrintStackTrace(e)))
	this.close()
	os.Exit(1)
}

func (this *ApplicationContext) Start() {
	if len(this.started) == 0 {
		this.startLifecycleBeans()
	}
	this.PublishEvent(NewContextStartedEvent())
}

func (this *ApplicationContext) Stop() {
	if len(this.started) > 0 {
		this.stopLifecycleBeans()
	}
	this.publishEvent(NewContextStoppedEvent(), true)
}

func (this *ApplicationContext) stopLifecycleBeans() {
	executor := concurrent.NewExecutor(runtime.NumCPU(), 0)
	defer executor.Shutdown()

	phaseToBeans := this.phaseToLifecycleBeans(this.started)
	this.started = nil
	sortedPhases := make([]int, 0, len(phaseToBeans))
	for phase := range phaseToBeans {
		sortedPhases = append(sortedPhases, phase)
	}
	sort.Ints(sortedPhases)

	for _, phase := range collections.ReverseSlice(sortedPhases) {
		beans := phaseToBeans[phase]
		futures := make([]concurrent.Future[BeanDefinition], 0)
		for _, bean := range beans {
			futures = append(futures, concurrent.Submit(executor, func(_ context.Context) BeanDefinition {
				defer err.Recover(func(e any) {
					slog.Error(fmt.Sprintf("Could not stop Lifecycle bean %v. %s", bean, err.PrintStackTrace(e)))
				})
				bean.getInstance().(Lifecycle).Stop()
				return nil
			}))
		}
		for _, future := range futures {
			future.Result()
		}
	}
}

func (this *ApplicationContext) destroyBeans() {
	this.foreachBeanDefinition(collections.ReverseSlice(this.instantiatedSnapshot()),
		func(bean BeanDefinition) bool { return bean.preDestroyEligible() },
		func(bean BeanDefinition) {
			bean.preDestroy()
		})
}

func (this *ApplicationContext) PublishEvent(event any) {
	this.publishEvent(event, false)
}

func (this *ApplicationContext) publishEvent(event any, recoverPanic bool) {
	eventType := reflect.TypeOf(event)
	eventValue := reflect.ValueOf(event)
	listeners := this.eventListeners(eventType)
	for _, listener := range listeners {
		if recoverPanic {
			this.notifyEventListener(listener.beanDefinition, listener.instance, listener.method, eventValue)
		} else {
			listener.method.invoke(listener.instance, eventValue)
		}
	}
}

func (this *ApplicationContext) notifyEventListener(bean BeanDefinition, instance any, method eventListenerMethod, eventValue reflect.Value) {
	defer err.Recover(func(e any) {
		slog.Error(fmt.Sprintf("Notify failed for bean %v. %s", bean, err.PrintStackTrace(e)))
	})
	method.invoke(instance, eventValue)
}

func (this *ApplicationContext) eventListeners(eventType reflect.Type) []eventListener {
	listeners := this.eventListenersCache.GetOrDefault(eventType, nil)
	if listeners == nil {
		listeners = this.resolveEventListeners(eventType)
		this.eventListenersCache.Put(eventType, listeners)
	}
	return listeners
}

func (this *ApplicationContext) resolveEventListeners(eventType reflect.Type) []eventListener {
	listenerMethodsByBean := make(map[any][]eventListenerMethod)
	definitionByBean := make(map[any]BeanDefinition)

	orderedBeans := this.orderedBeanInstances(this.instantiatedSnapshot(), func(bean BeanDefinition) bool {
		instance := bean.getInstance()
		methods := bean.getEventListenerMethods(eventType)
		definitionByBean[instance] = bean
		listenerMethodsByBean[instance] = methods
		return len(methods) > 0
	})

	listeners := make([]eventListener, 0)

	for _, bean := range orderedBeans {
		instance := bean.getInstance()
		for _, method := range listenerMethodsByBean[instance] {
			listeners = append(listeners, eventListener{
				beanDefinition: definitionByBean[instance],
				instance:       instance,
				method:         method,
			})
		}
	}

	return listeners
}

func (this *ApplicationContext) foreachBeanDefinition(beans []BeanDefinition, filter func(b BeanDefinition) bool, do func(BeanDefinition)) {
	for _, bean := range beans {
		if filter(bean) {
			do(bean)
		}
	}
}

type eventListener struct {
	beanDefinition BeanDefinition
	instance       any
	method         eventListenerMethod
}
