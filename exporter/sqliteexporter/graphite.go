package sqliteexporter

import (
	"regexp"
	"strconv"
	"strings"
)

func parseAliasByNode(expr string) (string, []int, bool) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "aliasByNode(") || !strings.HasSuffix(expr, ")") {
		return "", nil, false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(expr, "aliasByNode("), ")")
	args := splitTopLevelCSV(inner)
	if len(args) < 2 {
		return "", nil, false
	}

	pattern := strings.TrimSpace(args[0])
	pattern = strings.Trim(pattern, "\"'")

	idxs := make([]int, 0, len(args)-1)
	for _, a := range args[1:] {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		idx, err := strconv.Atoi(a)
		if err != nil {
			return "", nil, false
		}
		idxs = append(idxs, idx)
	}
	if len(idxs) == 0 {
		return "", nil, false
	}
	return pattern, idxs, true
}

func splitTopLevelCSV(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

func aliasByNode(metric string, idxs []int) string {
	parts := strings.Split(metric, ".")
	if len(parts) == 0 {
		return metric
	}

	selected := make([]string, 0, len(idxs))
	for _, idx := range idxs {
		p := idx
		if p < 0 {
			p = len(parts) + p
		}
		if p < 0 || p >= len(parts) {
			continue
		}
		selected = append(selected, parts[p])
	}
	if len(selected) == 0 {
		return metric
	}
	return strings.Join(selected, ".")
}

// parseAliasSub parses aliasSub(metric, "search", "replace") expressions
func parseAliasSub(expr string) (string, string, string, bool) {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "aliasSub(") || !strings.HasSuffix(expr, ")") {
		return "", "", "", false
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(expr, "aliasSub("), ")")
	args := splitTopLevelCSV(inner)
	if len(args) != 3 {
		return "", "", "", false
	}

	metric := strings.TrimSpace(args[0])
	metric = strings.Trim(metric, "'\"")
	search := strings.TrimSpace(args[1])
	search = strings.Trim(search, "'\"")
	replace := strings.TrimSpace(args[2])
	replace = strings.Trim(replace, "'\"")

	return metric, search, replace, true
}

// aliasSubMaxLen limits the length of regex patterns to prevent ReDoS.
const aliasSubMaxLen = 512

// aliasSub performs regex substitution on metric names.
// Patterns longer than aliasSubMaxLen are rejected to mitigate ReDoS.
func aliasSub(metric string, searchRegex string, replace string) string {
	if len(searchRegex) > aliasSubMaxLen {
		return metric
	}
	re, err := regexp.Compile(searchRegex)
	if err != nil {
		return metric // Return original if regex is invalid
	}
	// Use a timeout via matched length limit as a cheap guard.
	return re.ReplaceAllString(metric, replace)
}

func extractServiceFromTags(tags string) string {
	// logfmt-ish: key=value key2="value with spaces"
	fields := strings.Fields(tags)
	for _, f := range fields {
		kv := strings.SplitN(f, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.Trim(strings.TrimSpace(kv[1]), "\"")
		if key == "service.name" || key == "resource.service.name" {
			return val
		}
	}
	return ""
}

// traceQLServiceRe matches service.name in TraceQL expressions
var traceQLServiceRe = regexp.MustCompile(`(?:resource\.)?service\.name\s*=\s*"([^"]+)"`)

func extractServiceFromTraceQL(q string) string {
	m := traceQLServiceRe.FindStringSubmatch(q)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func graphiteToLikePattern(query string) string {
	var builder strings.Builder
	builder.Grow(len(query))
	for _, r := range query {
		switch r {
		case '%', '_':
			builder.WriteRune('\\')
			builder.WriteRune(r)
		case '*':
			builder.WriteRune('%')
		case '?':
			builder.WriteRune('_')
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

// metricNameReplacer replaces invalid characters in metric names
var metricNameReplacer = strings.NewReplacer(
	" ", "_",
	"/", "_",
	"\\", "_",
	":", "_",
	"=", "_",
	";", "_",
	"(", "_",
	")", "_",
	"[", "_",
	"]", "_",
	"{", "_",
	"}", "_",
)

// sanitizeMetricName replaces invalid characters in metric names
func sanitizeMetricName(name string) string {
	return metricNameReplacer.Replace(name)
}
