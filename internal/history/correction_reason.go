package history

import "encoding/json"

func correctionBlockerReason(content string) string {
	var review struct {
		Findings string `json:"findings"`
	}
	if json.Unmarshal([]byte(content), &review) == nil && review.Findings != "" {
		return review.Findings
	}
	return content
}
