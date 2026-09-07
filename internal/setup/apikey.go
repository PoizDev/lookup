package setup

import "unicode/utf8"

func maskKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= 8 {
		return "••••••••"
	}
	return string(runes[:4]) + "••••••••" + string(runes[len(runes)-3:])
}

func secretLength(key string) int { return utf8.RuneCountInString(key) }

func removeLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}
