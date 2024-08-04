package utils

import (
	"strings"
)

func ContentType(contentType string) string {
	return strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
}
