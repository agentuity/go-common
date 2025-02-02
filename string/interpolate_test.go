package string

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInterpolateStrings(t *testing.T) {
	type testCase struct {
		input       string
		env         []map[string]interface{}
		expectedVal string
		expectedErr error
	}

	testCases := []testCase{
		{input: "abc", env: nil, expectedVal: "abc", expectedErr: nil},
		{input: "", env: nil, expectedVal: "", expectedErr: nil},
		{input: "this is a {test}", env: []map[string]interface{}{{"test": "TEST"}}, expectedVal: "this is a TEST", expectedErr: nil},
		{input: "this is a {test} {notfound}", env: []map[string]interface{}{{"test": "TEST"}}, expectedVal: "this is a TEST {notfound}", expectedErr: nil},
		{input: "this is a {test:-notfound}", env: []map[string]interface{}{{"foo": "TEST"}}, expectedVal: "this is a notfound", expectedErr: nil},
		{input: "this is a {test:-fail}", env: []map[string]interface{}{{"test": "TEST"}}, expectedVal: "this is a TEST", expectedErr: nil},
		{input: "this is a {test}", env: []map[string]interface{}{{"test": 123}}, expectedVal: "this is a 123", expectedErr: nil},
		{input: "this is a {test}", env: []map[string]interface{}{{"test": nil}}, expectedVal: "this is a {test}", expectedErr: nil},
		{input: "this is a {test}", env: []map[string]interface{}{{"test": ""}}, expectedVal: "this is a {test}", expectedErr: nil},
		{input: "this is a {!test}", env: []map[string]interface{}{{"test": ""}}, expectedVal: "", expectedErr: fmt.Errorf("required value not found for key 'test'")},
		{input: "this is a {!test}", env: []map[string]interface{}{{"test": nil}}, expectedVal: "", expectedErr: fmt.Errorf("required value not found for key 'test'")},
		{input: "this is a ${test}", env: []map[string]interface{}{{"test": nil}}, expectedVal: "this is a ${test}", expectedErr: nil},
		{input: "this is a ${test:-foo}", env: []map[string]interface{}{{"test": "foo"}}, expectedVal: "this is a foo", expectedErr: nil},
	}

	for _, tc := range testCases {
		actualVal, actualErr := InterpolateString(tc.input, tc.env...)
		assert.Equal(t, tc.expectedVal, actualVal)
		assert.Equal(t, tc.expectedErr, actualErr)
	}
}
