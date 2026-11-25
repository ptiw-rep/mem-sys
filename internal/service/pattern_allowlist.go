package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
)

// ProjectPatterns mirrors the JSON output of tools/module_pattern_gen.py.
// Saved as project_pattern.json, it is the source of truth for which module
// path patterns are allowed in the system.
type ProjectPatterns struct {
	Root         string   `json:"root"`
	ModuleCount  int      `json:"module_count"`
	PathPatterns []string `json:"path_patterns"`
}

// Allowlist holds the allowed module path patterns and can test whether a
// given path (or pattern) is permitted. Loaded once at startup from
// project_pattern.json.
type Allowlist struct {
	patterns []string
}

// NewAllowlist reads project_pattern.json and builds an allowlist.
func NewAllowlist(filePath string) (*Allowlist, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read pattern file: %w", err)
	}
	var pp ProjectPatterns
	if err := json.Unmarshal(data, &pp); err != nil {
		return nil, fmt.Errorf("parse pattern file: %w", err)
	}
	if len(pp.PathPatterns) == 0 {
		return nil, fmt.Errorf("pattern file contains no path_patterns: %s", filePath)
	}
	return &Allowlist{patterns: pp.PathPatterns}, nil
}

// IsAllowed reports whether the given file path matches any allowed pattern.
func (a *Allowlist) IsAllowed(filePath string) bool {
	if a == nil {
		return true // no allowlist configured -> allow everything
	}
	for _, pat := range a.patterns {
		if matchGlob(pat, filePath) {
			return true
		}
	}
	return false
}

// AllowedModules returns a copy of the raw allowed patterns (for audit/display).
func (a *Allowlist) AllowedModules() []string {
	if a == nil {
		return nil
	}
	out := make([]string, len(a.patterns))
	copy(out, a.patterns)
	return out
}

// PatternAllowed reports whether a module path pattern itself is in the
// allowlist. Used when creating or updating modules to reject patterns that
// are not authorized.
func (a *Allowlist) PatternAllowed(pattern string) bool {
	if a == nil {
		return true
	}
	for _, pat := range a.patterns {
		if pat == pattern || matchGlob(pat, pattern) {
			return true
		}
	}
	return false
}

// matchGlob is a small glob matcher supporting ** (any depth), * (one
// segment), and ? (single char). It is intentionally dependency-free.
func matchGlob(pattern, name string) bool {
	// Normalize both sides.
	pattern = strings.TrimSpace(pattern)
	name = strings.TrimSpace(name)

	// Trailing /** matches that directory and everything beneath it.
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		if prefix == "" {
			return true // "/**" matches everything
		}
		return name == prefix || strings.HasPrefix(name, prefix+"/")
	}

	// ** anywhere else: split into prefix/suffix around the first **.
	if strings.Contains(pattern, "**") {
		parts := strings.SplitN(pattern, "**", 2)
		prefix := parts[0]
		suffix := ""
		if len(parts) == 2 {
			suffix = parts[1]
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			return false
		}
		if suffix != "" && !strings.HasSuffix(name, suffix) {
			return false
		}
		return true
	}

	// Plain segment-by-segment match (supports * and ? within a segment).
	return matchSegments(pattern, name)
}

func matchSegments(pattern, name string) bool {
	pParts := strings.Split(pattern, "/")
	nParts := strings.Split(name, "/")
	if len(pParts) != len(nParts) {
		return false
	}
	for i := range pParts {
		if !matchSegment(pParts[i], nParts[i]) {
			return false
		}
	}
	return true
}

func matchSegment(p, n string) bool {
	if p == "*" {
		return true
	}
	if strings.ContainsAny(p, "*?") {
		ok, err := path.Match(p, n)
		if err != nil {
			return false
		}
		return ok
	}
	return p == n
}
