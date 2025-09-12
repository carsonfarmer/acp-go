package main

import "strings"

// toMultiLineComment converts a multi-line string to Go comment format
func toMultiLineComment(s string) string {
	if s == "" {
		return ""
	}
	return "// " + strings.ReplaceAll(s, "\n", "\n// ") + "\n"
}

// toTitleCase converts snake_case, kebab-case, or camelCase string to TitleCase
func toTitleCase(s string) string {
	if s == "" {
		return ""
	}
	
	var words []string
	
	// Handle snake_case and kebab-case
	if strings.Contains(s, "_") || strings.Contains(s, "-") {
		s = strings.ReplaceAll(s, "-", "_")
		words = strings.Split(s, "_")
	} else {
		// Handle camelCase - split on uppercase letters
		var word strings.Builder
		for i, r := range s {
			if i > 0 && (r >= 'A' && r <= 'Z') {
				words = append(words, word.String())
				word.Reset()
			}
			word.WriteRune(r)
		}
		if word.Len() > 0 {
			words = append(words, word.String())
		}
	}
	
	// Capitalize each word
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + strings.ToLower(word[1:])
		}
	}
	
	return strings.Join(words, "")
}

