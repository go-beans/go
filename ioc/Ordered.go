package ioc

// ApplicationRunner(s) order. Default: math.MaxInt
//
// lower order - executes first
//
// higher order - executes later
type Ordered interface {
	Order() int
}
