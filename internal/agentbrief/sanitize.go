package agentbrief

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

var (
	absolutePathPattern = regexp.MustCompile(`(^|[[:space:]"'(=:])/(?:[A-Za-z0-9._-]+/?)+`)
	relativePathPattern = regexp.MustCompile(`(^|[[:space:]"'(=:])(?:internal|cmd|scripts|docs|openspec|\.github)/(?:[A-Za-z0-9._-]+/?)+`)
	digestPattern       = regexp.MustCompile(`\b(?:[A-Fa-f0-9]{64}|[A-Fa-f0-9]{40})\b`)
	assignedSecret      = regexp.MustCompile(`(?i)\b(?:token|secret|password|claim[-_]?handle)[[:space:]]*[:=][[:space:]]*[^[:space:],;}\]"']+`)
	sensitiveKeyPattern = regexp.MustCompile(`(?i)^(?:token|secret|password|claim[-_]?handle)$`)
)

func sanitizeNarrative(value string) string {
	value = absolutePathPattern.ReplaceAllString(value, `${1}[redacted-path]`)
	value = relativePathPattern.ReplaceAllString(value, `${1}[redacted-path]`)
	value = digestPattern.ReplaceAllString(value, `[redacted-digest]`)
	return assignedSecret.ReplaceAllString(value, `[redacted-secret]`)
}

func sanitizeBody(body json.RawMessage) json.RawMessage {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return json.RawMessage(`null`)
	}
	value = sanitizeJSONValue(value)
	encoded, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return encoded
}

func sanitizeJSONValue(value any) any {
	switch typed := value.(type) {
	case string:
		return sanitizeNarrative(typed)
	case []any:
		for index := range typed {
			typed[index] = sanitizeJSONValue(typed[index])
		}
	case map[string]any:
		result := make(map[string]any, len(typed))
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, originalKey := range keys {
			key, item := originalKey, typed[originalKey]
			key = sanitizeNarrative(key)
			base := key
			for suffix := 2; ; suffix++ {
				if _, exists := result[key]; !exists {
					break
				}
				key = fmt.Sprintf("%s#%d", base, suffix)
			}
			if sensitiveKeyPattern.MatchString(key) {
				result[key] = "[redacted-secret]"
			} else {
				result[key] = sanitizeJSONValue(item)
			}
		}
		return result
	}
	return value
}
