package string

import (
	"encoding/json"
)

// JSONStringify converts any value to a JSON string.
func JSONStringify(val any, pretty ...bool) string {
	var buf []byte
	var err error
	if len(pretty) > 0 && pretty[0] {
		buf, err = json.MarshalIndent(val, "", "  ")
	} else {
		buf, err = json.Marshal(val)
	}
	if err != nil {
		panic(err)
	}
	return string(buf)
}
