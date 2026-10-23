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
	initializationReady atomic.Bool
	eventListenersCache *concurrent.HashMap[reflect.Type, []eventListener]
	refreshing          atomic.Bool
	definitionsResolved atomic.Bool
	refreshed           atomic.Bool
	startTime           time.Time
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
	slog.Info(fmt.Sprintf("Starting application (PID %d)", os.Getpid()))
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
		return this.beanByName(inject)
	} else if inject.t.Kind() == reflect.Slice {
		return this.beanSlice(inject)
	} else {
		return this.beanByType(inject)
	}
}

func (this *ApplicationContext) beanByName(inject *InjectQualifier[any]) any {
	bean, ok := this.named[inject.name]
	if !ok && inject.optional {
		return nil
	}

	lang.Assert(ok, "No bean named '%s' found", inject.name)
	return this.beanInstance(bean)
}

func (this *ApplicationContext) beanSlice(inject *InjectQualifier[any]) any {
	elemType := inject.t.Elem()

	orderedBeans := this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
		return this.eligible(bean.getType(), elemType)
	})

	result := reflect.MakeSlice(inject.t, 0, len(orderedBeans))
	for _, bean := range orderedBeans {
		instance := this.beanInstance(bean)
		value := reflect.ValueOf(instance)

		lang.Assert(value.IsValid() && value.Type().AssignableTo(elemType), "Bean %s is not assignable to %s", bean, elemType)
		result = reflect.Append(result, value)
	}

	return result.Interface()
}

func (this *ApplicationContext) beanByType(inject *InjectQualifier[any]) any {
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

func (this *ApplicationContext) beanInstance(bean BeanDefinition) any {
	return this.beanInstanceWithPath(bean, []BeanDefinition{bean})
}

func (this *ApplicationContext) beanInstanceWithPath(bean BeanDefinition, path []BeanDefinition) (instance any) {
	defer err.Catch(func(e any) {
		panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error creating bean %v", bean), e))
	})

	if bean.getScope() == Singleton {
		if bean.getInstance() == nil {
			concurrent.Synchronized(bean.getMutex(), func() {
				if bean.getInstance() == nil {
					instance := bean.instantiate()
					bean.setInstance(instance)
					bean.addInstance(instance)
					this.addInstantiated(bean)
					this.eventListenersCache.Clear()
				}
			})
		}
		instance = bean.getInstance()
		if bean.tryInjection() {
			this.instantiateDependencies(bean, path)
			bean.injectDependencies(instance)
			if this.initializationReady.Load() {
				instance = this.initializeBean(bean, instance)
				this.reinjectDependencies()
			}
		}
	} else {
		concurrent.Synchronized(bean.getMutex(), func() {
			instance = bean.instantiate()
			this.instantiateDependencies(bean, path)
			bean.injectDependencies(instance)
			instance = this.initializeBean(bean, instance)
		})
	}

	return instance
}

func (this *ApplicationContext) instantiateDependencies(bean BeanDefinition, path []BeanDefinition) {
	for _, name := range bean.getDependsOn() {
		dependency, ok := this.named[name]
		lang.Assert(ok, "No dependency bean named '%s' found", name)

		for _, visited := range path {
			if visited == dependency {
				names := make([]string, 0, len(path)+1)
				for _, item := range path {
					names = append(names, item.getNames()[0])
				}
				names = append(names, dependency.getNames()[0])
				panic(err.NewIllegalStateException(fmt.Sprintf("Circular DependsOn dependency: %s", strings.Join(names, " -> "))))
			}
		}

		this.beanInstanceWithPath(dependency, append(path, dependency))
	}
}

func (this *ApplicationContext) addInstantiated(bean BeanDefinition) {
	this.instantiatedMu.Lock()
	this.instantiated = append(this.instantiated, bean)
	this.instantiatedMu.Unlock()

	slog.Debug(fmt.Sprintf("Created %v", bean))
}

func (this *ApplicationContext) getInstantiated() []BeanDefinition {
	this.instantiatedMu.RLock()
	defer this.instantiatedMu.RUnlock()

	return append([]BeanDefinition(nil), this.instantiated...)
}

func (this *ApplicationContext) eligible(registered, requested reflect.Type) bool {
	return registered.AssignableTo(requested)
}

func (this *ApplicationContext) refresh() {
	defer err.Recover(func(e any) {
		this.exit1(e, "Application context refresh failed.")
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

	slog.Info(fmt.Sprintf("Application context refreshed in %v (%d beans)", time.Since(threshold).Round(100*time.Microsecond), len(this.getInstantiated())))
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
		// slog.Debug(fmt.Sprintf("Registered %v", bean))
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
	this.initializePostProcessors()

	nonLazySingletons := this.orderedBeanInstances(this.resolved, func(bean BeanDefinition) bool {
		return bean.getScope() == Singleton && !bean.isLazy() && !bean.isBeanPostProcessor()
	})
	for _, bean := range nonLazySingletons {
		this.initializeBean(bean, bean.getInstance())
	}
	for i := 0; i < len(this.getInstantiated()); i++ {
		bean := this.getInstantiated()[i]
		if bean.isLazy() && !bean.isBeanPostProcessor() {
			this.initializeBean(bean, bean.getInstance())
		}
	}
	this.reinjectDependencies()
	this.initializationReady.Store(true)
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
			if !processor.compareAndSwapInitializationState(BeanNotInitialized, BeanInitializing) {
				return
			}
			defer processor.completeInitialization()

			processor.initialize(processor.getInstance())
			processor.setInitializationState(BeanInitialized)
		}, func(e any) {
			panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error initializing bean %v", processor), e))
		})
	}
}

func (this *ApplicationContext) initializeBean(bean BeanDefinition, instance any) any {
	if bean.getScope() == Singleton {
		if !bean.compareAndSwapInitializationState(BeanNotInitialized, BeanInitializing) {
			return bean.getInstance()
		}
		defer bean.completeInitialization()
	}

	defer err.Catch(func(e any) {
		panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error initializing bean %v", bean), e))
	})

	for _, name := range bean.getDependsOn() {
		dependency, ok := this.named[name]
		lang.Assert(ok, "No dependency bean named '%s' found", name)
		if dependency.getScope() == Singleton {
			this.beanInstance(dependency)
			this.initializeBean(dependency, dependency.getOriginalInstance())
			dependency.waitForInitialization()
			lang.Assert(dependency.getInitializationState() == BeanInitialized, "Dependency bean %s initialization failed", dependency)
		}
	}

	originalInstance := instance
	instance = this.postProcessBeforeInitialization(bean, instance)
	bean.initialize(originalInstance)
	instance = this.postProcessAfterInitialization(bean, instance)

	if bean.getScope() == Singleton {
		bean.setInstance(instance)
		bean.setInitializationState(BeanInitialized)
	}

	return instance
}

func (this *ApplicationContext) postProcessBeforeInitialization(bean BeanDefinition, instance any) any {
	for _, processor := range this.postProcessors {
		replacement := processor.getInstance().(BeanPostProcessor).PostProcessBeforeInitialization(instance, bean.getNames()[0])
		if replacement == nil {
			break
		}
		if reflect.TypeOf(instance) == reflect.TypeOf(replacement) && reflect.TypeOf(instance).Comparable() && instance == replacement {
			continue
		}
		lang.Assert(reflect.TypeOf(replacement).AssignableTo(bean.getType()), "BeanPostProcessor %T returned incompatible replacement %T for bean '%s' of type %s", processor.getInstance(), replacement, bean.getNames()[0], bean.getType())

		instance = replacement
		if bean.getScope() == Singleton {
			bean.addInstance(instance)
		}
		bean.injectDependencies(instance)
	}

	return instance
}

func (this *ApplicationContext) postProcessAfterInitialization(bean BeanDefinition, instance any) any {
	for _, processor := range this.postProcessors {
		replacement := processor.getInstance().(BeanPostProcessor).PostProcessAfterInitialization(instance, bean.getNames()[0])
		if replacement == nil {
			break
		}
		if reflect.TypeOf(instance) == reflect.TypeOf(replacement) && reflect.TypeOf(instance).Comparable() && instance == replacement {
			continue
		}
		lang.Assert(reflect.TypeOf(replacement).AssignableTo(bean.getType()), "BeanPostProcessor %T returned incompatible replacement %T for bean '%s' of type %s", processor.getInstance(), replacement, bean.getNames()[0], bean.getType())

		instance = replacement
		if bean.getScope() == Singleton {
			bean.addInstance(instance)
		}
		bean.injectDependencies(instance)
	}

	return instance
}

func (this *ApplicationContext) reinjectDependencies() {
	for _, bean := range this.getInstantiated() {
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
		this.exit1(e, "Application context run failed.")
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
			slog.Info(fmt.Sprintf("Closing application context (%d beans)", len(this.getInstantiated())))
			this.publishEvent(NewContextClosedEvent(), true)

			this.cancel()
			this.stopLifecycleBeans()
			this.destroyBeans()
			resetBeanDefinitions()

			slog.Info(fmt.Sprintf("Application context closed in %v (uptime %v)", time.Since(threshold).Round(time.Microsecond), time.Since(this.startTime).Round(10*time.Millisecond)))
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
	this.foreachBeanDefinition(collections.ReverseSlice(this.getInstantiated()),
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
	listenerMethodsByBean := make(map[BeanDefinition][]eventListenerMethod)

	orderedBeans := this.orderedBeanInstances(this.getInstantiated(), func(bean BeanDefinition) bool {
		methods := bean.getEventListenerMethods(eventType)
		listenerMethodsByBean[bean] = methods
		return len(methods) > 0
	})

	listeners := make([]eventListener, 0)

	for _, bean := range orderedBeans {
		for _, method := range listenerMethodsByBean[bean] {
			listeners = append(listeners, eventListener{
				beanDefinition: bean,
				instance:       bean.getInstance(),
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
