package ioc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/lang"
	"github.com/go-external-config/go/util/concurrent"
	"github.com/go-external-config/go/util/err"
)

var applicationContext atomic.Pointer[ApplicationContext]
var applicationContextMu sync.Mutex

type ApplicationContext struct {
	context       context.Context
	cancel        context.CancelFunc
	beans         map[reflect.Type][]BeanDefinition
	named         map[string]BeanDefinition
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
		context:   context,
		cancel:    cancel,
		beans:     make(map[reflect.Type][]BeanDefinition),
		named:     make(map[string]BeanDefinition),
		startTime: time.Now(),
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

func (this *ApplicationContext) beanInstance(bean BeanDefinition) (instance any) {
	defer err.Recover(func(err any) {
		slog.Error(fmt.Sprintf("Could not initialize bean %v\n%v\n%s", bean, err, debug.Stack()))
		this.Close()
		os.Exit(1)
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
				}
			})
			return bean.getInstance()
		}
		return bean.getInstance()
	}
	concurrent.Synchronized(bean.getMutex(), func() {
		instance = bean.instantiate()
	})
	return instance
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
	this.foreachBean(func(bean BeanDefinition) {
		if bean.getScope() == Singleton && !bean.isLazy() {
			this.beanInstance(bean)
		}
	})
}

func (this *ApplicationContext) Close() {
	concurrent.Synchronized(&applicationContextMu, func() {
		if applicationContext.CompareAndSwap(this, nil) {
			startTheshold := time.Now()
			slog.Info(fmt.Sprintf("ioc.ApplicationContext: closing context with %d running services", this.servicesCount.Load()))
			this.cancel()
			this.foreachBean(func(bean BeanDefinition) {
				if bean.preDestroyEligible() {
					slog.Debug(fmt.Sprintf("ioc.ApplicationContext: destroying %v", bean))
					bean.preDestroy()
				}
			})
			slog.Info(fmt.Sprintf("ioc.ApplicationContext: context closed in %v, uptime %v", time.Since(startTheshold), time.Since(this.startTime)))
		}
	})
}

func (this *ApplicationContext) foreachBean(do func(BeanDefinition)) {
	for _, beans := range this.beans {
		for _, bean := range beans {
			do(bean)
		}
	}
}
