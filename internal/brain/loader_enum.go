// Code generated by go-enum DO NOT EDIT.
// Version:
// Revision:
// Build Date:
// Built By:

package brain

import (
	"errors"
	"fmt"
)

const (
	// ProviderOpenai is a Provider of type openai.
	ProviderOpenai Provider = "openai"
	// ProviderBedrock is a Provider of type bedrock.
	ProviderBedrock Provider = "bedrock"
	// ProviderOllama is a Provider of type ollama.
	ProviderOllama Provider = "ollama"
	// ProviderGoogle is a Provider of type google.
	ProviderGoogle Provider = "google"
)

var ErrInvalidProvider = errors.New("not a valid Provider")

// String implements the Stringer interface.
func (x Provider) String() string {
	return string(x)
}

// IsValid provides a quick way to determine if the typed value is
// part of the allowed enumerated values
func (x Provider) IsValid() bool {
	_, err := ParseProvider(string(x))
	return err == nil
}

var _ProviderValue = map[string]Provider{
	"openai":  ProviderOpenai,
	"bedrock": ProviderBedrock,
	"ollama":  ProviderOllama,
	"google":  ProviderGoogle,
}

// ParseProvider attempts to convert a string to a Provider.
func ParseProvider(name string) (Provider, error) {
	if x, ok := _ProviderValue[name]; ok {
		return x, nil
	}
	return Provider(""), fmt.Errorf("%s is %w", name, ErrInvalidProvider)
}

// MarshalText implements the text marshaller method.
func (x Provider) MarshalText() ([]byte, error) {
	return []byte(string(x)), nil
}

// UnmarshalText implements the text unmarshaller method.
func (x *Provider) UnmarshalText(text []byte) error {
	tmp, err := ParseProvider(string(text))
	if err != nil {
		return err
	}
	*x = tmp
	return nil
}
