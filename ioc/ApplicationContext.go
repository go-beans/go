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
	registered          []BeanDefinition
	instantiated        []BeanDefinition
	started             []BeanDefinition
	beans               map[reflect.Type][]BeanDefinition
	named               map[string]BeanDefinition
	eventListenersCache map[reflect.Type][]eventListener
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
	slog.Info(fmt.Sprintf("ioc.ApplicationContext: starting with PID %d", os.Getpid()))
	context, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return &ApplicationContext{
		context:             context,
		cancel:              cancel,
		registered:          make([]BeanDefinition, 0),
		instantiated:        make([]BeanDefinition, 0),
		beans:               make(map[reflect.Type][]BeanDefinition),
		named:               make(map[string]BeanDefinition),
		eventListenersCache: make(map[reflect.Type][]eventListener),
		startTime:           time.Now(),
	}
}

func (this *ApplicationContext) register(bean BeanDefinition) {
	if env.MatchesProfiles(bean.getProfiles()...) {
		if len(bean.getNames()) > 0 {
			for _, name := range bean.getNames() {
				_, ok := this.named[name]
				lang.Assert(!ok, "Bean with name '%s' already registered", name)
				this.named[name] = bean
			}
		}
		this.beans[bean.getType()] = append(this.beans[bean.getType()], bean)
		this.registered = append(this.registered, bean)
		slog.Debug(fmt.Sprintf("ioc.ApplicationContext: registered %s", bean))
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
	if len(inject.name) > 0 {
		bean, ok := this.named[inject.name]
		if !ok && inject.optional {
			return nil
		}
		lang.Assert(ok, "No bean named '%s' found", inject.name)
		return this.beanInstance(bean)

	} else if inject.t.Kind() == reflect.Slice {
		elemType := inject.t.Elem()
		orderedBeans := this.orderedBeanInstances(this.registered, func(bean BeanDefinition) bool {
			return this.eligible(bean.getType(), elemType)
		})
		result := reflect.MakeSlice(inject.t, 0, 0)
		for _, bean := range orderedBeans {
			value := reflect.ValueOf(bean)
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
	defer err.Catch(func(e any) {
		panic(err.NewRuntimeExceptionFrom(fmt.Sprintf("Error creating bean %v", bean), e))
	})
	for _, name := range bean.getDependsOn() {
		bean, ok := this.named[name]
		lang.Assert(ok, "No dependency bean named '%s' found", name)
		this.beanInstance(bean)
	}
	if bean.getScope() == Singleton {
		if bean.getInstance() == nil {
			concurrent.Synchronized(bean.getMutex(), func() {
				if bean.getInstance() == nil {
					this.servicesCount.Add(1)
					bean.instantiate()
					this.instantiated = append(this.instantiated, bean)
					this.eventListenersCache = make(map[reflect.Type][]eventListener)
				}
			})
		}
		return bean.getInstance()
	}
	return bean.instantiate()
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
	this.initializeBeans()
	this.startLifecycleBeans()
	this.refreshed.Store(true)

	slog.Info(fmt.Sprintf("ioc.ApplicationContext: context refreshed in %v", time.Since(threshold)))
	this.PublishEvent(NewContextRefreshedEvent(this))
}

func (this *ApplicationContext) initializeBeans() {
	this.foreachBeanDefinition(this.registered, func(bean BeanDefinition) bool {
		return bean.getScope() == Singleton && !bean.isLazy()
	}, func(bean BeanDefinition) {
		this.beanInstance(bean)
	})
}

func (this *ApplicationContext) startLifecycleBeans() {
	executor := concurrent.NewExecutor[BeanDefinition](runtime.NumCPU())
	defer executor.Close()

	phaseToBeans := this.phaseToLifecycleBeans(this.registered)
	sortedPhases := make([]int, 0, len(phaseToBeans))
	for phase := range phaseToBeans {
		sortedPhases = append(sortedPhases, phase)
	}
	sort.Ints(sortedPhases)

	var err error
	for _, phase := range sortedPhases {
		beans := phaseToBeans[phase]
		futures := make([]concurrent.Future[BeanDefinition], 0)
		for _, bean := range beans {
			futures = append(futures, executor.Submit(func() BeanDefinition {
				this.beanInstance(bean).(Lifecycle).Start()
				return bean
			}))
		}
		for _, future := range futures {
			bean, e := future.Result()
			if e == nil {
				this.started = append(this.started, bean)
			} else if err == nil {
				err = e
			}
		}
		if err != nil {
			panic(err)
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

func (this *ApplicationContext) orderedBeanInstances(beans []BeanDefinition, filter func(b BeanDefinition) bool) []any {
	orderToBeans := make(map[int][]any)
	this.foreachBeanDefinition(beans, filter,
		func(bean BeanDefinition) {
			instance := this.beanInstance(bean)
			order := math.MaxInt
			if bean.getOrder() != nil {
				order = *bean.getOrder()
			} else if bean.isOrdered() {
				order = instance.(Ordered).Order()
			}
			beans, ok := orderToBeans[order]
			if !ok {
				beans = make([]any, 0)
			}
			beans = append(beans, instance)
			orderToBeans[order] = beans
		})

	sortedOrder := make([]int, 0, len(orderToBeans))
	for order := range orderToBeans {
		sortedOrder = append(sortedOrder, order)
	}
	sort.Ints(sortedOrder)

	orderedBeans := make([]any, 0)
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
	orderedBeans := this.orderedBeanInstances(this.registered, func(bean BeanDefinition) bool {
		return bean.isApplicationRunner()
	})
	for _, bean := range orderedBeans {
		bean.(ApplicationRunner).Run(os.Args)
	}
}

func (this *ApplicationContext) close() {
	concurrent.Synchronized(&applicationContextMu, func() {
		if this.closing.CompareAndSwap(false, true) {
			threshold := time.Now()
			slog.Info(fmt.Sprintf("ioc.ApplicationContext: closing context with %d running services", this.servicesCount.Load()))
			this.publishEvent(NewContextClosedEvent(), true)

			this.cancel()
			this.stopLifecycleBeans()
			this.destroyBeans()

			slog.Info(fmt.Sprintf("ioc.ApplicationContext: context closed in %v, uptime %v", time.Since(threshold), time.Since(this.startTime)))
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
	executor := concurrent.NewExecutor[BeanDefinition](runtime.NumCPU())
	defer executor.Close()

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
			futures = append(futures, executor.Submit(func() BeanDefinition {
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
	this.foreachBeanDefinition(collections.ReverseSlice(this.instantiated),
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
	listeners, ok := this.eventListenersCache[eventType]
	if ok {
		return listeners
	}

	listeners = this.resolveEventListeners(eventType)
	this.eventListenersCache[eventType] = listeners
	return listeners
}

func (this *ApplicationContext) resolveEventListeners(eventType reflect.Type) []eventListener {
	listenerMethodsByBean := make(map[any][]eventListenerMethod)
	definitionByBean := make(map[any]BeanDefinition)

	orderedBeans := this.orderedBeanInstances(this.instantiated, func(bean BeanDefinition) bool {
		instance := bean.getInstance()
		methods := bean.getEventListenerMethods(eventType)
		definitionByBean[instance] = bean
		listenerMethodsByBean[instance] = methods
		return len(methods) > 0
	})

	listeners := make([]eventListener, 0)

	for _, instance := range orderedBeans {
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
