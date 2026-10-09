package ioc

import (
	"strings"

	"github.com/go-external-config/go/env"
	"github.com/go-jang/go/lang"
)

type PropertyCondition struct {
	name  string
	value *string
}

func NewPropertyCondition(name string, value ...string) *PropertyCondition {
	lang.Assert(name != "", "Property name must not be empty")
	lang.Assert(len(value) <= 1, "ConditionalOnProperty accepts at most one value")

	this := &PropertyCondition{name: name}
	if len(value) == 1 {
		this.value = &value[0]
	}

	return this
}

func (this *PropertyCondition) Matches() bool {
	if !env.HasProperty(this.name) {
		return this.value == nil && envHasPropertyWithPrefix(this.name)
	}

	if this.value == nil {
		return true
	}

	return env.Property(this.name) == *this.value
}

func envHasPropertyWithPrefix(name string) bool {
	dotPrefix := name + "."
	arrayPrefix := name + "["

	for _, source := range env.PropertySources() {
		for key := range source.Properties() {
			if strings.HasPrefix(key, dotPrefix) || strings.HasPrefix(key, arrayPrefix) {
				return true
			}
		}
	}

	return false
}
