package string

import "strings"

// StringPointer will set the pointer to nil if the string is not nil but an empty string
func StringPointer(v string) *string {
	if v != "" {
		nv := strings.TrimSpace(v)
		if nv == "" {
			return nil
		} else {
			return &nv
		}
	}
	return nil
}

// ClearEmptyStringPointer will set the pointer to nil if the string is not nil but an empty string
func ClearEmptyStringPointer(v *string) *string {
	if v != nil {
		nv := strings.TrimSpace(*v)
		if nv == "" {
			return nil
		} else {
			return &nv
		}
	}
	return nil
}
