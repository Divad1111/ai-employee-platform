package workflowmcp

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var digitRe = regexp.MustCompile(`\d+`)

// ParseSemver 解析语义化版本号，不足三位补 0。
func ParseSemver(version string) (major, minor, patch int) {
	parts := digitRe.FindAllString(version, -1)
	nums := make([]int, 0, 3)
	for _, p := range parts {
		n, _ := strconv.Atoi(p)
		nums = append(nums, n)
	}
	for len(nums) < 3 {
		nums = append(nums, 0)
	}
	return nums[0], nums[1], nums[2]
}

// FormatSemver 格式化版本号。
func FormatSemver(major, minor, patch int) string {
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// CompareVersions 比较两个版本：-1 / 0 / 1。
func CompareVersions(left, right string) int {
	lm, ln, lp := ParseSemver(left)
	rm, rn, rp := ParseSemver(right)
	if lm != rm {
		if lm < rm {
			return -1
		}
		return 1
	}
	if ln != rn {
		if ln < rn {
			return -1
		}
		return 1
	}
	if lp < rp {
		return -1
	}
	if lp > rp {
		return 1
	}
	return 0
}

// BumpVersion 按 part 递增版本（major/minor/patch）。
func BumpVersion(version, part string) string {
	major, minor, patch := ParseSemver(version)
	switch strings.ToLower(part) {
	case "major":
		return FormatSemver(major+1, 0, 0)
	case "minor":
		return FormatSemver(major, minor+1, 0)
	default:
		return FormatSemver(major, minor, patch+1)
	}
}

// ResolveSavedVersion 决定 upsert 后的版本号。
// 新建采用 incoming；若 incoming > current 采用 incoming；否则 bump。
func ResolveSavedVersion(current, incoming string, isNew bool, bump string) string {
	if isNew {
		if incoming == "" {
			return "1.0.0"
		}
		return incoming
	}
	if current == "" {
		current = "1.0.0"
	}
	if incoming == "" {
		incoming = current
	}
	if CompareVersions(incoming, current) > 0 {
		return incoming
	}
	return BumpVersion(current, bump)
}
