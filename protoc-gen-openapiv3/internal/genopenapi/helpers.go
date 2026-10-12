//go:build go1.12
// +build go1.12

package genopenapi

import (
	"strconv"
	"strings"
	"unicode"
)

// this method will filter the same fields and return the unique one
func getUniqueFields(schemaFieldsRequired []string, fieldsRequired []string) []string {
	var unique []string
	var index *int

	for j, schemaFieldRequired := range schemaFieldsRequired {
		index = nil
		for i, fieldRequired := range fieldsRequired {
			i := i
			if schemaFieldRequired == fieldRequired {
				index = &i
				break
			}
		}
		if index == nil {
			unique = append(unique, schemaFieldsRequired[j])
		}
	}
	return unique
}

func toPascalCase(s string) string {
	if s == "" {
		return ""
	}

	var builder strings.Builder
	capitalizeNext := true

	for _, r := range s {
		if r == '_' || r == '.' || r == '-' {
			capitalizeNext = true
			continue
		}

		if capitalizeNext {
			builder.WriteRune(unicode.ToUpper(r))
			capitalizeNext = false
		} else {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}

// stringEnum returns the values of a string enum.
func stringEnum(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, v := range values {
		out = append(out, v)
	}
	return out
}

// integerEnum returns the annotated enum values of an integer as JSON numbers. It returns nil
// when a value is not an integer, so a wrong annotation adds no constraint.
func integerEnum(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, v := range values {
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// booleanEnum returns the annotated enum values of a boolean as JSON booleans. It returns nil
// when a value is not true or false.
func booleanEnum(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, v := range values {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil
		}
		out = append(out, b)
	}
	return out
}
