package db

import (
	"fmt"
	"strings"
)

func inPlaceholders(count int) (string, error) {
	if count <= 0 {
		return "", fmt.Errorf("placeholder count must be positive")
	}
	parts := make([]string, count)
	for index := range parts {
		parts[index] = "?"
	}
	return strings.Join(parts, ","), nil
}

func int64Args(values []int64) []any {
	args := make([]any, 0, len(values))
	for _, value := range values {
		args = append(args, value)
	}
	return args
}
