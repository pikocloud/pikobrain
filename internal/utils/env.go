package utils

import (
	"fmt"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

type Value[T any] struct {
	Value   *T     `json:"value" yaml:"value"`
	FromEnv string `json:"from_env" yaml:"fromEnv"`
}

func (v *Value[T]) Get() (T, error) {
	if v.Value != nil {
		return *v.Value, nil
	}

	var out T
	if reflect.TypeFor[T]().Kind() == reflect.String {
		var x any = os.Getenv(v.FromEnv)
		return x.(T), nil
	}
	err := yaml.Unmarshal([]byte(os.Getenv(v.FromEnv)), &out)
	if err != nil {
		return out, fmt.Errorf("get from env %q: %w", v.FromEnv, err)
	}
	return out, nil
}

type Pair[T any] struct {
	Name     string `json:"name" yaml:"name"`
	Value[T] `yaml:",inline"`
}
