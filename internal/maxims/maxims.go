package maxims

import (
	"fmt"
	"strings"

	pitcrew "github.com/fmazzalomo/pitcrew"
)

type Maxim struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Principle string `json:"principle"`
}

func Text() string { return pitcrew.MaximsText }

func Structured() ([]Maxim, error) {
	roman := map[string]int{"I": 1, "II": 2, "III": 3, "IV": 4}
	lines := strings.Split(Text(), "\n")
	result := make([]Maxim, 0, 4)
	for index, line := range lines {
		if !strings.HasPrefix(line, "## Maxim ") {
			continue
		}
		heading := strings.TrimPrefix(line, "## Maxim ")
		parts := strings.SplitN(heading, " — ", 2)
		if len(parts) != 2 || roman[parts[0]] != len(result)+1 {
			return nil, fmt.Errorf("invalid maxim heading %q", line)
		}
		principle := ""
		for cursor := index + 1; cursor < len(lines); cursor++ {
			candidate := strings.TrimSpace(lines[cursor])
			if strings.HasPrefix(candidate, "## Maxim ") {
				break
			}
			if strings.HasPrefix(candidate, "> **") && strings.HasSuffix(candidate, "**") {
				principle = strings.TrimSuffix(strings.TrimPrefix(candidate, "> **"), "**")
				break
			}
		}
		if principle == "" {
			return nil, fmt.Errorf("maxim %s has no principle", parts[0])
		}
		result = append(result, Maxim{Number: roman[parts[0]], Title: parts[1], Principle: principle})
	}
	if len(result) != 4 {
		return nil, fmt.Errorf("expected four maxims, found %d", len(result))
	}
	return result, nil
}
