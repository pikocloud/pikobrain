// Code generated by go-enum DO NOT EDIT.
// Version:
// Revision:
// Build Date:
// Built By:

package loader

import (
	"errors"
	"fmt"
)

const (
	// OpenAPI is a ToolType of type OpenAPI.
	OpenAPI ToolType = "openapi"
)

var ErrInvalidToolType = errors.New("not a valid ToolType")

// String implements the Stringer interface.
func (x ToolType) String() string {
	return string(x)
}

// IsValid provides a quick way to determine if the typed value is
// part of the allowed enumerated values
func (x ToolType) IsValid() bool {
	_, err := ParseToolType(string(x))
	return err == nil
}

var _ToolTypeValue = map[string]ToolType{
	"openapi": OpenAPI,
}

// ParseToolType attempts to convert a string to a ToolType.
func ParseToolType(name string) (ToolType, error) {
	if x, ok := _ToolTypeValue[name]; ok {
		return x, nil
	}
	return ToolType(""), fmt.Errorf("%s is %w", name, ErrInvalidToolType)
}

// MarshalText implements the text marshaller method.
func (x ToolType) MarshalText() ([]byte, error) {
	return []byte(string(x)), nil
}

// UnmarshalText implements the text unmarshaller method.
func (x *ToolType) UnmarshalText(text []byte) error {
	tmp, err := ParseToolType(string(text))
	if err != nil {
		return err
	}
	*x = tmp
	return nil
}