package ioc

// Phase for Lifecycle beans. Default: 0
//
// phase 0 - normal components
//
// negative phases - infrastructure
//
// positive phases - late-start services
type Phased interface {
	Phase() int
}
