package ioc

type ApplicationContextAware interface {
	SetApplicationContext(ctx *ApplicationContext)
}
