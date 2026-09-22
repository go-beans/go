package ioc

type BeanPostProcessor interface {
	PostProcessBeforeInitialization(bean any, name string) any
	PostProcessAfterInitialization(bean any, name string) any
}
