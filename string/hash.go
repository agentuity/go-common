package string

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	xxhash "github.com/cespare/xxhash/v2"
	gstr "github.com/savsgio/gotils/strconv"
)

func NewHash64(val ...interface{}) uint64 {
	sha := xxhash.New()
	for _, v := range val {
		switch r := v.(type) {
		case string:
			sha.WriteString(r)
		case int, int8, int16, int32, int64:
			sha.WriteString(fmt.Sprintf("%d", r))
		case float32, float64:
			sha.WriteString(fmt.Sprintf("%f", r))
		case bool:
			sha.WriteString(strconv.FormatBool(r))
		default:
			buf, _ := json.Marshal(r)
			sha.Write(buf)
		}
	}
	return sha.Sum64()
}

// NewHash returns a hash of one or more input variables using xxhash algorithm
func NewHash(val ...interface{}) string {
	v := fmt.Sprintf("%x", NewHash64(val...))
	if len(v) == 16 {
		return v
	}
	return strings.Repeat("0", 16-len(v)) + v
}

// FNV1Hash will take a string and return a FNV-1 hash value as a uint32
func FNV1Hash(val string) uint32 {
	h := fnv.New32()
	h.Write([]byte(val))
	return h.Sum32()
}

// Modulo will take the value and return a modulo with the num length
func Modulo(value string, num int) int {
	hasher := fnv.New32a()
	hasher.Write(gstr.S2B(value))
	partition := int(hasher.Sum32()) % num
	if partition < 0 {
		partition = -partition
	}
	return partition
}
