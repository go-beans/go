package ioc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"reflect"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	con "github.com/go-beans/go/concurrent"
	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/lang"
	"github.com/go-external-config/go/util/collection"
	"github.com/go-external-config/go/util/concurrent"
	"github.com/go-external-config/go/util/err"
)

var applicationContext atomic.Pointer[ApplicationContext]
var applicationContextMu sync.Mutex

type ApplicationContext struct {
	context       context.Context
	cancel        context.CancelFunc
	registered    []BeanDefinition
	instantiated  []BeanDefinition
	started       []BeanDefinition
	beans         map[reflect.Type][]BeanDefinition
	named         map[string]BeanDefinition
	refreshed     atomic.Bool
	startTime     time.Time
	servicesCount atomic.Int32
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
	context, cancel := context.WithCancel(context.Background())
	return &ApplicationContext{
		context:      context,
		cancel:       cancel,
		registered:   make([]BeanDefinition, 0),
		instantiated: make([]BeanDefinition, 0),
		beans:        make(map[reflect.Type][]BeanDefinition),
		named:        make(map[string]BeanDefinition),
		startTime:    time.Now(),
	}
}

func (this *ApplicationContext) Register(bean BeanDefinition) {
	if env.MatchesProfiles(bean.getProfiles()...) {
		slog.Debug(fmt.Sprintf("ioc.ApplicationContext: registering %s", bean))
		if len(bean.getNames()) > 0 {
			for _, name := range bean.getNames() {
				_, ok := this.named[name]
				lang.AssertState(!ok, "Bean with name '%s' already registered", name)
				this.named[name] = bean
			}
		}
		this.beans[bean.getType()] = append(this.beans[bean.getType()], bean)
		this.registered = append(this.registered, bean)
	}
}

func (this *ApplicationContext) Bean(inject *InjectQualifier[any]) any {
	if len(inject.name) > 0 {
		bean, ok := this.named[inject.name]
		lang.AssertState(ok, "No bean named '%s' found", inject.name)
		return this.beanInstance(bean)
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

		lang.AssertState(len(primaryCandidates) <= 1, "Multiple primary beans of type %v found. Use name qualifier.\n%v", inject.t, primaryCandidates)
		if len(primaryCandidates) == 1 {
			return this.beanInstance(primaryCandidates[0])
		} else {
			lang.AssertState(len(candidates) > 0, "No bean of type %v found", inject.t)
			lang.AssertState(len(candidates) <= 1, "Multiple beans of type %v found. Use name qualifier or mark one of the beans primary.\n%v", inject.t, candidates)
			return this.beanInstance(candidates[0])
		}
	}
}

func (this *ApplicationContext) beanInstance(bean BeanDefinition) any {
	defer err.Recover(func(err any) {
		this.doExit(err, "Could not initialize bean %v\n%v", bean, err)
	})
	for _, name := range bean.getDependsOn() {
		bean, ok := this.named[name]
		lang.AssertState(ok, "No dependency bean named '%s' found", name)
		this.beanInstance(bean)
	}
	if bean.getScope() == Singleton {
		if bean.getInstance() == nil {
			concurrent.Synchronized(bean.getMutex(), func() {
				if bean.getInstance() == nil {
					this.servicesCount.Add(1)
					bean.instantiate()
					this.instantiated = append(this.instantiated, bean)
				}
			})
		}
		return bean.getInstance()
	}
	return bean.instantiate()
}

func (this *ApplicationContext) eligible(registered, requested reflect.Type) bool {
	if registered.AssignableTo(requested) {
		return true
	}
	if requested.Kind() == reflect.Interface && registered.Implements(requested) {
		return true
	}

	if registered.Kind() == reflect.Pointer {
		elem := registered.Elem()
		if elem.AssignableTo(requested) {
			return true
		}
		if requested.Kind() == reflect.Interface && elem.Implements(requested) {
			return true
		}
	} else {
		ptr := reflect.PointerTo(registered)
		if ptr.AssignableTo(requested) {
			return true
		}
		if requested.Kind() == reflect.Interface && ptr.Implements(requested) {
			return true
		}
	}

	return false
}

func (this *ApplicationContext) Refresh() {
	defer err.Recover(func(err any) {
		this.doExit(err, "Context refresh failed: %v", err)
	})

	threshold := time.Now()
	this.initializeBeans()
	this.startLifecycleBeans()
	this.refreshed.Store(true)

	slog.Info(fmt.Sprintf("ioc.ApplicationContext: context refreshed in %v", time.Since(threshold)))
	this.notifyContextRefreshed()
}

func (this *ApplicationContext) initializeBeans() {
	this.foreachBean(this.registered, func(bean BeanDefinition) bool {
		return bean.getScope() == Singleton && !bean.isLazy()
	}, func(bean BeanDefinition) int {
		return 0
	}, func(bean BeanDefinition) {
		this.beanInstance(bean)
	})
}

func (this *ApplicationContext) startLifecycleBeans() {
	executor := con.NewExecutor[BeanDefinition](runtime.NumCPU())
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
		futures := make([]con.Future[BeanDefinition], 0)
		for _, bean := range beans {
			futures = append(futures, executor.Submit(func() BeanDefinition {
				this.beanInstance(bean).(Lifecycle).Start()
				return bean
			}))
		}
		for _, future := range futures {
			b, e := future.Result()
			if e == nil {
				this.started = append(this.started, b)
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
	this.foreachBean(beans, func(bean BeanDefinition) bool {
		return bean.isLifecycleBean()
	}, func(bean BeanDefinition) int {
		return 0
	}, func(bean BeanDefinition) {
		var phase int
		if bean.getPhase() != nil {
			phase = *bean.getPhase()
		} else if bean.isPhased() {
			phase = this.beanInstance(bean).(Phased).Phase()
		}
		phased, ok := phaseToBeans[phase]
		if !ok {
			phased = make([]BeanDefinition, 0)
		}
		phased = append(phased, bean)
		phaseToBeans[phase] = phased
	})
	return phaseToBeans
}

func (this *ApplicationContext) notifyContextRefreshed() {
	this.foreachBean(this.instantiated, func(bean BeanDefinition) bool {
		_, ok := bean.getInstance().(ContextRefreshedListener)
		return ok
	}, func(bean BeanDefinition) int {
		return 0
	}, func(bean BeanDefinition) {
		bean.getInstance().(ContextRefreshedListener).OnContextRefreshed()
	})
}

func (this *ApplicationContext) Run() {
	defer err.Recover(func(err any) {
		this.doExit(err, "Context run failed: %v", err)
	})

	if !this.refreshed.Load() {
		this.Refresh()
	}
	this.executeApplicationRunnerBeans()
	this.notifyApplicationReady()
}

func (this *ApplicationContext) executeApplicationRunnerBeans() {
	this.foreachBean(this.registered, func(bean BeanDefinition) bool {
		return bean.isApplicationRunner()
	}, func(bean BeanDefinition) int {
		if bean.getOrder() != nil {
			return *bean.getOrder()
		} else if bean.isOrdered() {
			return this.beanInstance(bean).(Ordered).Order()
		}
		return math.MaxInt
	}, func(bean BeanDefinition) {
		this.beanInstance(bean).(ApplicationRunner).Run(os.Args)
	})
}

func (this *ApplicationContext) notifyApplicationReady() {
	this.foreachBean(this.instantiated, func(bean BeanDefinition) bool {
		_, ok := bean.getInstance().(ApplicationReadyListener)
		return ok
	}, func(bean BeanDefinition) int {
		return 0
	}, func(bean BeanDefinition) {
		bean.getInstance().(ApplicationReadyListener).OnApplicationReady()
	})
}

func (this *ApplicationContext) Close() {
	concurrent.Synchronized(&applicationContextMu, func() {
		if applicationContext.CompareAndSwap(this, nil) {
			theshold := time.Now()
			slog.Info(fmt.Sprintf("ioc.ApplicationContext: closing context with %d running services", this.servicesCount.Load()))

			this.cancel()

			this.stopLifecycleBeans()
			this.destroyBeans()

			slog.Info(fmt.Sprintf("ioc.ApplicationContext: context closed in %v, uptime %v", time.Since(theshold), time.Since(this.startTime)))
		}
	})
}

func (this *ApplicationContext) exit(code int, format string, a ...any) {
	panic(NewExitCodeErrorWithStack(code, fmt.Sprintf(format, a...), nil, err.StackTrace(2)))
}

func (this *ApplicationContext) doExit(e any, format string, a ...any) {
	var exitCodeError *ExitCodeError
	if e1, ok := e.(error); ok && errors.As(e1, &exitCodeError) {
		slog.Error(err.PrintStackTrace(exitCodeError))
		this.Close()
		os.Exit(exitCodeError.code)
	} else {
		slog.Error(fmt.Sprintf("%v\n%s", fmt.Sprintf(format, a...), err.PrintStackTrace(e)))
		this.Close()
		os.Exit(1)
	}
}

func (this *ApplicationContext) stopLifecycleBeans() {
	executor := con.NewExecutor[BeanDefinition](runtime.NumCPU())
	defer executor.Close()

	phaseToBeans := this.phaseToLifecycleBeans(this.started)
	sortedPhases := make([]int, 0, len(phaseToBeans))
	for phase := range phaseToBeans {
		sortedPhases = append(sortedPhases, phase)
	}
	sort.Ints(sortedPhases)

	for _, phase := range collection.ReverseSlice(sortedPhases) {
		beans := phaseToBeans[phase]
		futures := make([]con.Future[BeanDefinition], 0)
		for _, bean := range beans {
			futures = append(futures, executor.Submit(func() BeanDefinition {
				defer err.Recover(func(err any) {
					slog.Error(fmt.Sprintf("Could not stop Lifecycle bean %v\n%v\n%s", bean, err, debug.Stack()))
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
	this.foreachBean(collection.ReverseSlice(this.instantiated),
		func(bean BeanDefinition) bool { return bean.preDestroyEligible() },
		func(b BeanDefinition) int { return 0 },
		func(bean BeanDefinition) {
			bean.preDestroy()
		})
}

func (this *ApplicationContext) notifyApplicationFailed() {
	this.foreachBean(this.instantiated, func(bean BeanDefinition) bool {
		_, ok := bean.getInstance().(ApplicationFailedListener)
		return ok
	}, func(bean BeanDefinition) int {
		return 0
	}, func(bean BeanDefinition) {
		defer err.Recover(func(err any) {
			slog.Error(fmt.Sprintf("Notification processing failed for bean %v\n%v\n%s", bean, err, debug.Stack()))
		})
		bean.getInstance().(ApplicationFailedListener).OnApplicationFailed()
	})
}

func (this *ApplicationContext) foreachBean(beans []BeanDefinition, filter func(b BeanDefinition) bool, order func(b BeanDefinition) int, do func(BeanDefinition)) {
	filtered := make([]BeanDefinition, 0)
	for _, bean := range beans {
		if filter(bean) {
			filtered = append(filtered, bean)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return order(filtered[i]) < order(filtered[j])
	})
	for _, bean := range filtered {
		do(bean)
	}
}
