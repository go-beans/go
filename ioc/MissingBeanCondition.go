package ioc

import (
	"reflect"

	"github.com/go-jang/go/lang"
)

type MissingBeanCondition struct {
	t    reflect.Type
	name string
}

func NewMissingBeanCondition(t reflect.Type, name ...string) *MissingBeanCondition {
	lang.Assert(t != nil, "Bean type must not be nil")
	lang.Assert(len(name) <= 1, "ConditionalOnMissingBean accepts at most one name")

	this := &MissingBeanCondition{t: t}
	if len(name) == 1 {
		this.name = name[0]
	}
	return this
}

func (this *MissingBeanCondition) Matches(beans []BeanDefinition) bool {
	for _, bean := range beans {
		if !bean.getType().AssignableTo(this.t) {
			continue
		}
		if this.name == "" {
			return false
		}
		for _, name := range bean.getNames() {
			if name == this.name {
				return false
			}
		}
	}
	return true
}
