package ioc

import (
	"fmt"
	"log/slog"
	"reflect"

	"github.com/go-external-config/go/lang"
)

type ApplicationContext struct {
	context map[reflect.Type][]BeanDefinition
	named   map[string]BeanDefinition
}

func newApplicationContext() *ApplicationContext {
	return &ApplicationContext{
		context: make(map[reflect.Type][]BeanDefinition),
		named:   make(map[string]BeanDefinition),
	}
}

func (c *ApplicationContext) Register(bean BeanDefinition) {
	slog.Debug(fmt.Sprintf("%T: registering %s", *c, bean))
	if len(bean.getName()) > 0 {
		_, ok := c.named[bean.getName()]
		lang.AssertState(!ok, "Bean with name '%s' already registered", bean.getName())
		c.named[bean.getName()] = bean
	}
	c.context[bean.getType()] = append(c.context[bean.getType()], bean)
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

		for t, beans := range c.context {
			if c.eligible(t, inject.t) {
				candidates = append(candidates, beans...)
				for _, bean := range beans {
					if bean.isPrimary() {
						primaryCandidates = append(primaryCandidates, bean)
					}
				}
			}
		}

		lang.AssertState(len(primaryCandidates) <= 1, "Multiple primary beans of type %v. Use name qualifier.\nFound: %v", inject.t, primaryCandidates)
		if len(primaryCandidates) == 1 {
			instance = c.beanInstance(primaryCandidates[0])
		} else {
			lang.AssertState(len(candidates) > 0, "No bean of type %v found", inject.t)
			lang.AssertState(len(candidates) <= 1, "Multiple beans of type %v. Use name qualifier or mark one of the beans primary.\nFound: %v", inject.t, candidates)
			instance = c.beanInstance(candidates[0])
		}
	}

	return instance
}

func (c *ApplicationContext) beanInstance(bean BeanDefinition) any {
	if bean.getScope() == Singleton {
		if bean.getInstance() == nil {
			slog.Debug(fmt.Sprintf("%T: instantiate %s", *c, bean))
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
