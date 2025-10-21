//go:build go1.12
// +build go1.12

package genopenapi

import (
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
		if r == '_' {
			capitalizeNext = true
			continue // Skip the underscore itself
		}

		if capitalizeNext {
			builder.WriteRune(unicode.ToUpper(r))
			capitalizeNext = false // Reset the flag
		} else {
			builder.WriteRune(r)
		}
	}

	return builder.String()
}
