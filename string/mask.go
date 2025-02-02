package string

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// Mask will mask a string by replacing the middle with asterisks.
func Mask(s string) string {
	l := len(s)
	if l == 0 {
		return s
	}
	if l == 1 {
		return "*"
	}
	h := int(l / 2)
	return s[0:h] + strings.Repeat("*", l-h)
}

// MaskURL returns a masked version of the URL string attempting to hide sensitive information.
func MaskURL(urlString string) (string, error) {
	u, err := url.Parse(urlString)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	var str strings.Builder
	str.WriteString(u.Scheme)
	str.WriteString("://")
	if u.User != nil {
		str.WriteString(Mask(u.User.Username()))
		pass, ok := u.User.Password()
		if ok {
			str.WriteString(":")
			str.WriteString(Mask(pass))
		}
		str.WriteString("@")
	}
	str.WriteString(u.Host)
	p := u.Path
	if p != "/" && p != "" {
		str.WriteString("/")
		if len(p) > 1 && p[0] == '/' {
			str.WriteString(Mask(p[1:]))
		}
	}
	var qs []string
	for k, v := range u.Query() {
		qs = append(qs, fmt.Sprintf("%s=%s", k, Mask(strings.Join(v, ","))))
	}
	sort.Strings(qs)
	if len(qs) > 0 {
		str.WriteString("?")
		str.WriteString(strings.Join(qs, "&"))
	}
	return str.String(), nil
}

// MaskEmail masks the email address attempting to hide sensitive information.
func MaskEmail(val string) string {
	tok := strings.Split(val, "@")
	dot := strings.Split(tok[1], ".")
	return Mask(tok[0]) + "@" + Mask(dot[0]) + "." + strings.Join(dot[1:], ".")
}

var isURL = regexp.MustCompile(`^(\w+)://`)
var isEmail = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
var isJWT = regexp.MustCompile(`^[a-zA-Z0-9-_]+\.[a-zA-Z0-9-_]+\.[a-zA-Z0-9-_]+$`)

// MaskArguments masks sensitive information in the given arguments.
func MaskArguments(args []string) []string {
	masked := make([]string, len(args))
	for i, arg := range args {
		if isURL.MatchString(arg) {
			u, err := MaskURL(arg)
			if err == nil {
				masked[i] = u
			} else {
				masked[i] = Mask(arg)
			}
		} else if isEmail.MatchString(arg) {
			masked[i] = MaskEmail(arg)
		} else if isJWT.MatchString(arg) {
			masked[i] = Mask(arg)
		} else {
			masked[i] = arg
		}
	}
	return masked
}
