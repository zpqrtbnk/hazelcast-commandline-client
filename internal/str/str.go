package str

import "strings"

// SplitByComma splits a string by commas, and optionally removes empty items.
func SplitByComma(str string, removeEmpty bool) []string {
	var idx int
	if str == "" {
		return nil
	}
	ls := strings.Split(str, ",")
	for _, s := range ls {
		s = strings.TrimSpace(s)
		if s != "" || !removeEmpty {
			ls[idx] = s
			idx++
		}
	}
	return ls[0:idx:idx]

}
