package ioc

import (
	"reflect"

	"github.com/go-jang/go/lang"
)

type BeanCondition struct {
	t    reflect.Type
	name string
}

func NewBeanCondition(t reflect.Type, name ...string) *BeanCondition {
	lang.Assert(t != nil, "Bean type must not be nil")
	lang.Assert(len(name) <= 1, "ConditionalOnBean accepts at most one name")

	this := &BeanCondition{t: t}
	if len(name) == 1 {
		this.name = name[0]
	}
	return this
}

func (this *BeanCondition) Matches(beans []BeanDefinition) bool {
	for _, bean := range beans {
		if !bean.getType().AssignableTo(this.t) {
			continue
		}

		if this.name == "" {
			return true
		}

		for _, name := range bean.getNames() {
			if name == this.name {
				return true
			}
		}
	}

	return false
}
