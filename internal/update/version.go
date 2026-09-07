package update

import (
	"fmt"
	"strconv"
	"strings"
)

type semVersion struct {
	major, minor, patch uint64
	prerelease          []string
}

func parseVersion(value string) (semVersion, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	if value == "" || strings.Contains(value, "+") {
		return semVersion{}, fmt.Errorf("invalid semantic version %q", value)
	}
	main, pre, _ := strings.Cut(value, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semVersion{}, fmt.Errorf("invalid semantic version %q", value)
	}
	numbers := make([]uint64, 3)
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semVersion{}, fmt.Errorf("invalid semantic version %q", value)
		}
		numbers[i] = n
	}
	v := semVersion{major: numbers[0], minor: numbers[1], patch: numbers[2]}
	if pre != "" {
		v.prerelease = strings.Split(pre, ".")
		for _, id := range v.prerelease {
			if id == "" {
				return semVersion{}, fmt.Errorf("invalid semantic version %q", value)
			}
		}
	}
	return v, nil
}

func Compare(a, b string) (int, error) {
	left, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	right, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for _, pair := range [][2]uint64{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1, nil
		}
		if pair[0] > pair[1] {
			return 1, nil
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) == 0 {
		return 0, nil
	}
	if len(left.prerelease) == 0 {
		return 1, nil
	}
	if len(right.prerelease) == 0 {
		return -1, nil
	}
	max := len(left.prerelease)
	if len(right.prerelease) > max {
		max = len(right.prerelease)
	}
	for i := 0; i < max; i++ {
		if i >= len(left.prerelease) {
			return -1, nil
		}
		if i >= len(right.prerelease) {
			return 1, nil
		}
		l, r := left.prerelease[i], right.prerelease[i]
		ln, le := strconv.ParseUint(l, 10, 64)
		rn, re := strconv.ParseUint(r, 10, 64)
		switch {
		case le == nil && re == nil && ln < rn:
			return -1, nil
		case le == nil && re == nil && ln > rn:
			return 1, nil
		case le == nil && re != nil:
			return -1, nil
		case le != nil && re == nil:
			return 1, nil
		case l < r:
			return -1, nil
		case l > r:
			return 1, nil
		}
	}
	return 0, nil
}

func IsReleaseVersion(value string) bool { _, err := parseVersion(value); return err == nil }
func IsPrerelease(value string) bool {
	v, err := parseVersion(value)
	return err == nil && len(v.prerelease) > 0
}
