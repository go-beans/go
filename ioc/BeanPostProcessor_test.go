package ioc_test

import (
	"testing"

	"github.com/go-beans/go/ioc"
	"github.com/stretchr/testify/require"
)

type ReinjectionDependency interface {
	Value() string
}

type ReinjectionDependencyImpl struct{}

func (this *ReinjectionDependencyImpl) Value() string {
	return "original"
}

type ReinjectionDependencyProxy struct {
	delegate ReinjectionDependency
}

func (this *ReinjectionDependencyProxy) Value() string {
	return this.delegate.Value() + ":proxied"
}

type ReinjectionService interface {
	Value() string
}

type ReinjectionServiceImpl struct {
	dependency ReinjectionDependency `inject:"reinjectionDependency"`
}

func (this *ReinjectionServiceImpl) Value() string {
	return this.dependency.Value()
}

type ReinjectionServiceCache struct {
	delegate   ReinjectionService
	dependency ReinjectionDependency `inject:"reinjectionDependency"`
}

func (this *ReinjectionServiceCache) Value() string {
	return this.delegate.Value() + ":" + this.dependency.Value()
}

type ReinjectionServiceProxy struct {
	delegate ReinjectionService
}

func (this *ReinjectionServiceProxy) Value() string {
	return this.delegate.Value()
}

type ReinjectionConsumer struct {
	service ReinjectionService `inject:"reinjectionService"`
}

type FirstReinjectionPostProcessor struct {
	dependency        ReinjectionDependency `inject:"reinjectionDependency"`
	initialDependency ReinjectionDependency
	cache             *ReinjectionServiceCache
}

func (this *FirstReinjectionPostProcessor) PostProcessBeforeInitialization(bean any, beanName string) any {
	return bean
}

func (this *FirstReinjectionPostProcessor) PostProcessAfterInitialization(bean any, beanName string) any {
	if beanName != "reinjectionService" {
		return bean
	}
	this.initialDependency = this.dependency
	this.cache = &ReinjectionServiceCache{delegate: bean.(ReinjectionService)}
	return this.cache
}

type SecondReinjectionPostProcessor struct {
	dependencyProxy *ReinjectionDependencyProxy
	serviceProxy    *ReinjectionServiceProxy
}

func (this *SecondReinjectionPostProcessor) PostProcessBeforeInitialization(bean any, beanName string) any {
	return bean
}

func (this *SecondReinjectionPostProcessor) PostProcessAfterInitialization(bean any, beanName string) any {
	switch beanName {
	case "reinjectionDependency":
		this.dependencyProxy = &ReinjectionDependencyProxy{delegate: bean.(ReinjectionDependency)}
		return this.dependencyProxy
	case "reinjectionService":
		this.serviceProxy = &ReinjectionServiceProxy{delegate: bean.(ReinjectionService)}
		return this.serviceProxy
	default:
		return bean
	}
}

func Test_BeanPostProcessorReinjection(t *testing.T) {
	dependency := ioc.Resolve[ReinjectionDependency]("reinjectionDependency")()
	service := ioc.Resolve[ReinjectionService]("reinjectionService")()
	consumer := ioc.Resolve[*ReinjectionConsumer]()()
	first := ioc.Resolve[*FirstReinjectionPostProcessor]()()
	second := ioc.Resolve[*SecondReinjectionPostProcessor]()()

	require.Same(t, second.dependencyProxy, dependency)
	require.Same(t, second.serviceProxy, service)
	require.Same(t, service, consumer.service)

	require.NotNil(t, first.cache)
	require.Same(t, first.cache, second.serviceProxy.delegate)
	require.IsType(t, &ReinjectionServiceImpl{}, first.cache.delegate)
	require.NotSame(t, service, first.cache.delegate)

	require.IsType(t, &ReinjectionDependencyImpl{}, first.initialDependency)
	require.Same(t, dependency, first.dependency)
	require.Same(t, dependency, first.cache.dependency)

	originalService := first.cache.delegate.(*ReinjectionServiceImpl)
	require.Same(t, dependency, originalService.dependency)
	require.IsType(t, &ReinjectionDependencyImpl{}, second.dependencyProxy.delegate)
	require.Equal(t, "original:proxied:original:proxied", consumer.service.Value())
}
