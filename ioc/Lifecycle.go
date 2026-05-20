package ioc

// see Phased
type Lifecycle interface {
	Start()
	Stop()
}
