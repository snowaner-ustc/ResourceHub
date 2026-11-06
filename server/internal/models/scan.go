package models

import (
	"fmt"
	"path/filepath"
	"strings"
)

func NormalizeScanRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if err := ValidateSlowPath(root); err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

func ValidateSlowPath(pattern string) error {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return fmt.Errorf("path required")
	}
	if !filepath.IsAbs(pattern) {
		return fmt.Errorf("path must be absolute")
	}
	prefix := staticPrefix(pattern)
	if prefix == "/" {
		return fmt.Errorf("refusing unbounded scan from /; pick a subdirectory")
	}
	for _, f := range []string{"/proc", "/sys", "/dev"} {
		if prefix == f || strings.HasPrefix(prefix, f+"/") {
			return fmt.Errorf("refusing to scan %s", f)
		}
	}
	return nil
}

func staticPrefix(pattern string) string {
	i := strings.IndexAny(pattern, "*?[")
	var p string
	if i < 0 {
		p = filepath.Clean(pattern)
	} else {
		p = filepath.Clean(pattern[:i])
	}
	if p == "." || p == "" {
		return "/"
	}
	return p
}

func QuotaRuleType(policyID, path string) string {
	return "quota:" + policyID + ":" + path
}

func QuotaRulePrefix(policyID string) string {
	return "quota:" + policyID + ":"
}
