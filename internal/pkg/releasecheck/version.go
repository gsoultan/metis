package releasecheck

import (
	"strconv"
	"strings"
)

// semver is the part of a release tag that orders it: v1.4.2 or v1.4.2-rc.1.
// Build metadata after + does not order and is dropped.
type semver struct {
	major, minor, patch int
	pre                 string
}

// parseTag reads a release tag. A build that is not a release — "dev", or
// git describe's v1.4.2-3-gabc1234 — is not a tag, and reports false.
func parseTag(tag string) (semver, bool) {
	s, ok := strings.CutPrefix(strings.TrimSpace(tag), "v")
	if !ok {
		return semver{}, false
	}
	s, _, _ = strings.Cut(s, "+")
	core, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 || (len(p) > 1 && p[0] == '0') {
			return semver{}, false
		}
		n[i] = v
	}
	if isDescribe(pre) {
		return semver{}, false
	}
	return semver{major: n[0], minor: n[1], patch: n[2], pre: pre}, true
}

// isDescribe is git describe's "N commits past the tag": "3-gabc1234".
func isDescribe(pre string) bool {
	count, rest, ok := strings.Cut(pre, "-")
	if !ok || !strings.HasPrefix(rest, "g") {
		return false
	}
	_, err := strconv.Atoi(count)
	return err == nil
}

// compare orders two versions; a pre-release sorts before its release.
func compare(a, b semver) int {
	for _, d := range [...]int{a.major - b.major, a.minor - b.minor, a.patch - b.patch} {
		if d != 0 {
			return sign(d)
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "":
		return 1
	case b.pre == "":
		return -1
	default:
		return comparePre(a.pre, b.pre)
	}
}

// comparePre orders pre-release identifiers as semantic versioning does:
// numbers numerically, words by bytes, numbers before words, shorter first.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aErr := strconv.Atoi(as[i])
		bn, bErr := strconv.Atoi(bs[i])
		switch {
		case aErr == nil && bErr == nil:
			if an != bn {
				return sign(an - bn)
			}
		case aErr == nil:
			return -1
		case bErr == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(as) - len(bs))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// CompareTags orders two release tags. ok is false when either is not a
// release tag, which cannot be ordered against one.
func CompareTags(a, b string) (order int, ok bool) {
	va, okA := parseTag(a)
	vb, okB := parseTag(b)
	if !okA || !okB {
		return 0, false
	}
	return compare(va, vb), true
}
