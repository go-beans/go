package ioc

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/go-external-config/go/env"
	"github.com/go-external-config/go/lang"
)

type ApplicationContext struct {
	ctx                map[reflect.Type][]BeanDefinition
	named              map[string]BeanDefinition
	preDestroyEligible []BeanDefinition
}

func newApplicationContext() *ApplicationContext {
	return &ApplicationContext{
		ctx:                make(map[reflect.Type][]BeanDefinition),
		named:              make(map[string]BeanDefinition),
		preDestroyEligible: make([]BeanDefinition, 0),
	}
}

func (c *ApplicationContext) Register(bean BeanDefinition) {
	if env.MatchesProfiles(bean.getProfiles()...) {
		slog.Info(fmt.Sprintf("%T: registering %s", *c, bean))
		if len(bean.getName()) > 0 {
			_, ok := c.named[bean.getName()]
			lang.AssertState(!ok, "Bean with name '%s' already registered", bean.getName())
			c.named[bean.getName()] = bean
		}
		c.ctx[bean.getType()] = append(c.ctx[bean.getType()], bean)
	}
}

func (c *ApplicationContext) Bean(inject *InjectQualifier[any]) any {
	var instance any
	if len(inject.name) > 0 {
		bean, ok := c.named[inject.name]
		lang.AssertState(ok, "No bean with name '%s' found", inject.name)
		instance = c.beanInstance(bean)
	} else {
		var candidates []BeanDefinition
		var primaryCandidates []BeanDefinition

		for t, beans := range c.ctx {
			if c.eligible(t, inject.t) {
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
			instance = c.beanInstance(primaryCandidates[0])
		} else {
			lang.AssertState(len(candidates) > 0, "No bean of type %v found", inject.t)
			lang.AssertState(len(candidates) <= 1, "Multiple beans of type %v found. Use name qualifier or mark one of the beans primary.\n%v", inject.t, candidates)
			instance = c.beanInstance(candidates[0])
		}
	}

	return instance
}

func (c *ApplicationContext) beanInstance(bean BeanDefinition) any {
	if bean.getScope() == Singleton {
		if bean.getInstance() == nil {
			slog.Debug(fmt.Sprintf("%T: instantiate %s", *c, bean))
			if bean.preDestroyEligible() {
				c.preDestroyEligible = append(c.preDestroyEligible, bean)
			}
			return bean.instantiate()
		}
		return bean.getInstance()
	}
	slog.Debug(fmt.Sprintf("%T: instantiate %s", *c, bean))
	return bean.instantiate()
}

func (c *ApplicationContext) eligible(registered, requested reflect.Type) bool {
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

func (c *ApplicationContext) Shutdown() {
	slog.Info(fmt.Sprintf("%T: shutdown started", *c))
	for i := len(c.preDestroyEligible) - 1; i >= 0; i-- {
		c.preDestroyEligible[i].preDestroy()
	}
	slog.Info(fmt.Sprintf("%T: shutdown finished", *c))
}
