package nvpm

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/mistweaverco/nvpm-client/internal/lib/registry_parser"
	"github.com/spf13/cobra"
)

// MatchShowJSON returns true when obj matches every filter (AND).
// Each filter is "[.]path:value". Leading "." is optional. Paths are
// dot-separated; arrays match if any element matches the remainder.
// Values use case-insensitive glob semantics (* and ?); without wildcards,
// matching is exact (after trim). Booleans accept true/false. '*' matches any
// characters including '/' and ':' (unlike path.Match). The first ':' in a
// filter separates path from value.
//
// Nested fields may be typed Go values from buildPackageInfoJSON
// (e.g. []map[string]string for git_refs, []string for categories), not only
// map[string]any / []any - matching uses reflection for maps and slices.
func MatchShowJSON(obj map[string]any, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	if obj == nil {
		return false
	}
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		ok, err := matchOneShowFilter(obj, f)
		if err != nil || !ok {
			return false
		}
	}
	return true
}

func matchOneShowFilter(obj map[string]any, filter string) (bool, error) {
	pathStr, value, err := parseShowFilter(filter)
	if err != nil {
		return false, err
	}
	segments := splitFilterPath(pathStr)
	if len(segments) == 0 {
		return false, fmt.Errorf("empty filter path")
	}
	return matchPathValue(obj, segments, value), nil
}

func parseShowFilter(filter string) (pathStr, value string, err error) {
	filter = strings.TrimSpace(filter)
	// Split on the first ':' only so values may contain colons (e.g. package_id:github:owner/repo).
	idx := strings.Index(filter, ":")
	if idx <= 0 {
		return "", "", fmt.Errorf("invalid filter %q (expected path:value)", filter)
	}
	pathStr = strings.TrimSpace(filter[:idx])
	value = strings.TrimSpace(filter[idx+1:])
	pathStr = strings.TrimPrefix(pathStr, ".")
	if pathStr == "" {
		return "", "", fmt.Errorf("empty filter path in %q", filter)
	}
	return pathStr, value, nil
}

func splitFilterPath(p string) []string {
	parts := strings.Split(p, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func matchPathValue(current any, segments []string, want string) bool {
	if len(segments) == 0 {
		return matchLeafValue(current, want)
	}
	if current == nil {
		return false
	}
	key := segments[0]
	rest := segments[1:]

	rv := reflect.ValueOf(current)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Map:
		child, ok := lookupReflectMapKeyCI(rv, key)
		if !ok {
			return false
		}
		return matchPathValue(child, rest, want)
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if matchPathValue(rv.Index(i).Interface(), segments, want) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func lookupReflectMapKeyCI(rv reflect.Value, key string) (any, bool) {
	if rv.Kind() != reflect.Map || rv.IsNil() {
		return nil, false
	}
	keyLower := strings.ToLower(key)
	for _, mk := range rv.MapKeys() {
		if mk.Kind() != reflect.String {
			continue
		}
		if strings.ToLower(mk.String()) == keyLower {
			return rv.MapIndex(mk).Interface(), true
		}
	}
	return nil, false
}

func matchLeafValue(got any, want string) bool {
	want = strings.TrimSpace(want)
	if got == nil {
		return false
	}
	switch v := got.(type) {
	case bool:
		b, err := strconv.ParseBool(want)
		if err != nil {
			return false
		}
		return v == b
	case float64:
		return matchGlobCI(formatJSONNumber(v), want)
	case float32:
		return matchGlobCI(formatJSONNumber(float64(v)), want)
	case int:
		return matchGlobCI(strconv.Itoa(v), want)
	case int64:
		return matchGlobCI(strconv.FormatInt(v, 10), want)
	case string:
		return matchGlobCI(v, want)
	}

	rv := reflect.ValueOf(got)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < rv.Len(); i++ {
			if matchLeafValue(rv.Index(i).Interface(), want) {
				return true
			}
		}
		return false
	case reflect.Map:
		// Nested object without remaining path segments: no scalar match.
		return false
	case reflect.String:
		return matchGlobCI(rv.String(), want)
	case reflect.Bool:
		b, err := strconv.ParseBool(want)
		if err != nil {
			return false
		}
		return rv.Bool() == b
	default:
		return matchGlobCI(fmt.Sprint(got), want)
	}
}

func formatJSONNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func matchGlobCI(got, pattern string) bool {
	got = strings.ToLower(strings.TrimSpace(got))
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	return matchFilterGlob(got, pattern)
}

// matchFilterGlob matches shell-like * and ? against the full string.
// Unlike path.Match, '*' matches any characters including '/' and ':',
// which is required for package_id values such as github:owner/repo.
func matchFilterGlob(got, pattern string) bool {
	return matchFilterGlobAt(got, pattern)
}

func matchFilterGlobAt(got, pattern string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			pattern = pattern[1:]
			if pattern == "" {
				return true
			}
			for i := 0; i <= len(got); i++ {
				if matchFilterGlobAt(got[i:], pattern) {
					return true
				}
			}
			return false
		case '?':
			if got == "" {
				return false
			}
			got = got[1:]
			pattern = pattern[1:]
		default:
			if got == "" || got[0] != pattern[0] {
				return false
			}
			got = got[1:]
			pattern = pattern[1:]
		}
	}
	return got == ""
}

// packageMatchesShowFilters builds show JSON for sourceID and applies filters.
func packageMatchesShowFilters(sourceID string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	item := newRegistryParser().GetBySourceId(sourceID)
	if item.Source.ID == "" {
		// Not in registry: synthesize a minimal object from lock/sourceID.
		obj := map[string]any{
			"package_id": sourceID,
			"name":       sourceID,
		}
		if strings.Contains(sourceID, ":") {
			parts := strings.SplitN(sourceID, ":", 2)
			if len(parts) == 2 {
				obj["provider"] = parts[0]
			}
		}
		if packageAlwaysTrustFromLock(sourceID) {
			obj["always_trust"] = true
			obj["status"] = "installed"
		}
		return MatchShowJSON(obj, filters)
	}
	return MatchShowJSON(buildPackageInfoJSON(item, sourceID), filters)
}

func registryItemMatchesShowFilters(item registry_parser.RegistryItem, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	sourceID := item.Source.ID
	return MatchShowJSON(buildPackageInfoJSON(item, sourceID), filters)
}

// filterSourceIDsByShowFilters keeps source IDs whose show JSON matches all filters.
func filterSourceIDsByShowFilters(ids []string, filters []string) []string {
	if len(filters) == 0 {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if packageMatchesShowFilters(id, filters) {
			out = append(out, id)
		}
	}
	return out
}

func getShowFilters(cmd *cobra.Command) []string {
	if cmd == nil {
		return nil
	}
	filters, err := cmd.Flags().GetStringArray("filter")
	if err != nil || len(filters) == 0 {
		return nil
	}
	out := make([]string, 0, len(filters))
	for _, f := range filters {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// registerShowFilterFlag adds the shared --filter DSL flag to a command.
func registerShowFilterFlag(cmd *cobra.Command) {
	cmd.Flags().StringArray("filter", nil, "Filter by show-JSON field (repeatable AND): [.]path:value with * ? globs")
}
