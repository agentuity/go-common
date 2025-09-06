package string

import (
	"strings"

	"github.com/agentuity/go-common/sys"
)

// StringPointer will set the pointer to nil if the string is not nil but an empty string
func StringPointer(v string) *string {
	if v != "" {
		nv := strings.TrimSpace(v)
		return sys.Ptr(nv)
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
			return sys.Ptr(nv)
		}
	}
	return nil
}
