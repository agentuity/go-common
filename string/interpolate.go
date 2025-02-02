package string

import (
	"fmt"
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`(\$?{(.*?)})`)

// InterpolateString replaces { } in string with values from environment maps.
func InterpolateString(val string, env ...map[string]interface{}) (string, error) {
	if val == "" {
		return val, nil
	}
	var err error
	val = re.ReplaceAllStringFunc(val, func(s string) string {
		tok := re.FindStringSubmatch(s)
		key := tok[2]
		def := s
		var required bool
		if strings.HasPrefix(key, "!") {
			key = key[1:]
			required = true
		}
		if idx := strings.Index(key, ":-"); idx != -1 {
			def = key[idx+2:]
			key = key[:idx]
		}
		var v interface{}
		for _, e := range env {
			if nv, ok := e[key]; ok {
				v = nv
				break
			}
		}
		if v == nil {
			if required {
				err = fmt.Errorf("required value not found for key '%s'", key)
			}
			return def
		}
		if v == "" {
			if required {
				err = fmt.Errorf("required value not found for key '%s'", key)
			}
			return def
		}
		return fmt.Sprint(v)
	})
	if err != nil {
		return "", err
	}
	return val, nil
}
