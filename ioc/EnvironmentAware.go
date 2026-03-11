package ioc

import "github.com/go-external-config/go/env"

// 1. Bean instantiation (finished)
//
// 2. Dependency injection (finished)
//
// 3. Aware callbacks (in-progress)
type EnvironmentAware interface {
	SetEnvironment(environment *env.Environment)
}
