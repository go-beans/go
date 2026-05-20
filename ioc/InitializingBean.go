package ioc

type InitializingBean interface {
	AfterPropertiesSet()
}
