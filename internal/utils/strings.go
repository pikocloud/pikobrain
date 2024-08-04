package utils

import (
	"strings"
)

func Concat(sep string, values ...string) string {
	var builder strings.Builder
	for _, value := range values {
		if value == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString(sep)
		}
		builder.WriteString(value)
	}
	return builder.String()
}
