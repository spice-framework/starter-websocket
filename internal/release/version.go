package release

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidateVersion checks the canonical v-prefixed SemVer accepted by release artifacts.
func ValidateVersion(version string) error {
	if !strings.HasPrefix(version, "v") || len(version) == 1 {
		return fmt.Errorf("version %q is not canonical v-prefixed semantic version", version)
	}
	value := version[1:]
	coreAndPre, build, hasBuild := strings.Cut(value, "+")
	if hasBuild && (!validIdentifiers(build, false) || strings.Contains(build, "+")) {
		return fmt.Errorf("version %q has invalid build metadata", version)
	}
	core, pre, hasPre := strings.Cut(coreAndPre, "-")
	if hasPre && !validIdentifiers(pre, true) {
		return fmt.Errorf("version %q has invalid prerelease metadata", version)
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return fmt.Errorf("version %q must contain major, minor, and patch", version)
	}
	for _, part := range parts {
		parsed, err := strconv.ParseUint(part, 10, 64)
		if err != nil || strconv.FormatUint(parsed, 10) != part {
			return fmt.Errorf("version %q has a non-canonical numeric component", version)
		}
	}
	return nil
}

func validIdentifiers(value string, rejectLeadingZero bool) bool {
	if value == "" {
		return false
	}
	for identifier := range strings.SplitSeq(value, ".") {
		if identifier == "" || rejectLeadingZero && len(identifier) > 1 && identifier[0] == '0' && allDigits(identifier) {
			return false
		}
		for _, character := range identifier {
			if !isIdentifierCharacter(character) {
				return false
			}
		}
	}
	return true
}

func isIdentifierCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '-'
}

func allDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}
