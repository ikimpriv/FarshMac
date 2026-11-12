package remotefs

import (
	"errors"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Minimal IDNA (RFC 3490/3492) conversion for host names typed by the user:
// NFC + lower case + Punycode per label. Net dialing and known_hosts use the
// ASCII form; Label shows the Unicode form.
const (
	punyBase        = 36
	punyTMin        = 1
	punyTMax        = 26
	punySkew        = 38
	punyDamp        = 700
	punyInitialBias = 72
	punyInitialN    = 128
)

var errInvalidHost = errors.New("invalid international host name")

func hostToASCII(host string) (string, error) {
	if isASCII(host) {
		return host, nil
	}
	labels := strings.Split(strings.ToLower(norm.NFC.String(host)), ".")
	for i, label := range labels {
		if isASCII(label) {
			continue
		}
		encoded, err := punyEncode(label)
		if err != nil {
			return "", err
		}
		labels[i] = "xn--" + encoded
		if len(labels[i]) > 63 {
			return "", errInvalidHost
		}
	}
	return strings.Join(labels, "."), nil
}
func hostToUnicode(host string) string {
	if !strings.Contains(strings.ToLower(host), "xn--") {
		return host
	}
	labels := strings.Split(host, ".")
	for i, label := range labels {
		if len(label) > 4 && strings.EqualFold(label[:4], "xn--") {
			if decoded, err := punyDecode(strings.ToLower(label[4:])); err == nil {
				labels[i] = decoded
			}
		}
	}
	return strings.Join(labels, ".")
}
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
func punyAdapt(delta, points int, first bool) int {
	if first {
		delta /= punyDamp
	} else {
		delta /= 2
	}
	delta += delta / points
	k := 0
	for delta > ((punyBase-punyTMin)*punyTMax)/2 {
		delta /= punyBase - punyTMin
		k += punyBase
	}
	return k + (punyBase-punyTMin+1)*delta/(delta+punySkew)
}
func punyDigit(d int) byte {
	if d < 26 {
		return byte('a' + d)
	}
	return byte('0' + d - 26)
}
func punyThreshold(k, bias int) int {
	switch {
	case k <= bias:
		return punyTMin
	case k >= bias+punyTMax:
		return punyTMax
	}
	return k - bias
}
func punyEncode(s string) (string, error) {
	runes := []rune(s)
	var out []byte
	for _, r := range runes {
		if r < 0x80 {
			out = append(out, byte(r))
		}
	}
	basic := len(out)
	handled := basic
	if basic > 0 {
		out = append(out, '-')
	}
	n, delta, bias := punyInitialN, 0, punyInitialBias
	for handled < len(runes) {
		m := rune(0x7fffffff)
		for _, r := range runes {
			if int(r) >= n && r < m {
				m = r
			}
		}
		if (int(m)-n)*(handled+1) > 1<<30 {
			return "", errInvalidHost
		}
		delta += (int(m) - n) * (handled + 1)
		n = int(m)
		for _, r := range runes {
			if int(r) < n {
				delta++
			}
			if int(r) == n {
				q := delta
				for k := punyBase; ; k += punyBase {
					t := punyThreshold(k, bias)
					if q < t {
						break
					}
					out = append(out, punyDigit(t+(q-t)%(punyBase-t)))
					q = (q - t) / (punyBase - t)
				}
				out = append(out, punyDigit(q))
				bias = punyAdapt(delta, handled+1, handled == basic)
				delta = 0
				handled++
			}
		}
		delta++
		n++
	}
	return string(out), nil
}
func punyDecode(s string) (string, error) {
	var output []rune
	pos := 0
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		for _, r := range s[:i] {
			if r >= 0x80 {
				return "", errInvalidHost
			}
			output = append(output, r)
		}
		pos = i + 1
	}
	n, i, bias := punyInitialN, 0, punyInitialBias
	for pos < len(s) {
		oldi, w := i, 1
		for k := punyBase; ; k += punyBase {
			if pos >= len(s) {
				return "", errInvalidHost
			}
			c := s[pos]
			pos++
			var digit int
			switch {
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			default:
				return "", errInvalidHost
			}
			i += digit * w
			if i > 1<<30 {
				return "", errInvalidHost
			}
			t := punyThreshold(k, bias)
			if digit < t {
				break
			}
			w *= punyBase - t
		}
		bias = punyAdapt(i-oldi, len(output)+1, oldi == 0)
		n += i / (len(output) + 1)
		i %= len(output) + 1
		if n > utf8.MaxRune {
			return "", errInvalidHost
		}
		output = append(output[:i], append([]rune{rune(n)}, output[i:]...)...)
		i++
	}
	return string(output), nil
}
