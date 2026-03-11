package ioc

// 1. Bean instantiation (finished)
//
// 2. Dependency injection (finished)
//
// 3. Aware callbacks (finished)
//
// 4. PostConstruct (finished)
//
// 5. InitializingBean.AfterPropertiesSet() (in-progress)
type InitializingBean interface {
	AfterPropertiesSet()
}
