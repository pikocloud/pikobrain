package utils

import (
	"encoding/json"
	"fmt"
)

func ConvertTo[T any](input any) (T, error) {
	var out T
	data, err := json.Marshal(input)
	if err != nil {
		return out, fmt.Errorf("marshal input: %w", err)
	}
	err = json.Unmarshal(data, &out)
	if err != nil {
		return out, fmt.Errorf("unmarshal input: %w", err)
	}
	return out, nil
}
