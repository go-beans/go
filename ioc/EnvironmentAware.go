package ioc

import "github.com/go-external-config/go/env"

type EnvironmentAware interface {
	SetEnvironment(environment *env.Environment)
}
