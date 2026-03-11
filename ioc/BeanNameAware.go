package ioc

// 1. Bean instantiation (finished)
//
// 2. Dependency injection (finished)
//
// 3. Aware callbacks (in-progress)
type BeanNameAware interface {
	SetBeanName(string)
}
