package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const maxInputBytes = 4 << 20

type flagRules struct {
	required   []string
	optional   []string
	repeatable []string
	boolean    []string
}

type flagValues map[string][]string

func parseFlags(args []string, rules flagRules) (flagValues, error) {
	allowed, repeat, boolean := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, name := range append(append(append([]string{}, rules.required...), rules.optional...), rules.repeatable...) {
		allowed[name] = true
	}
	for _, name := range rules.repeatable {
		repeat[name] = true
	}
	for _, name := range rules.boolean {
		allowed[name], boolean[name] = true, true
	}
	values := flagValues{}
	for i := 0; i < len(args); i++ {
		token := args[i]
		if !strings.HasPrefix(token, "--") || token == "--" {
			return nil, fmt.Errorf("%w: long-form flags only", ErrUsage)
		}
		name, value, hasValue := token, "", false
		if before, after, ok := strings.Cut(token, "="); ok {
			name, value, hasValue = before, after, true
		}
		if !allowed[name] {
			return nil, fmt.Errorf("%w: unknown flag %s", ErrUsage, name)
		}
		if boolean[name] {
			if hasValue {
				return nil, fmt.Errorf("%w: %s takes no value", ErrUsage, name)
			}
			value = "true"
		} else if !hasValue {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return nil, fmt.Errorf("%w: %s requires a value", ErrUsage, name)
			}
			i++
			value = args[i]
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%w: %s requires a non-empty value", ErrUsage, name)
		}
		if len(values[name]) != 0 && !repeat[name] {
			return nil, fmt.Errorf("%w: duplicate flag %s", ErrUsage, name)
		}
		values[name] = append(values[name], value)
	}
	for _, name := range rules.required {
		if len(values[name]) == 0 {
			return nil, fmt.Errorf("%w: missing required flag %s", ErrUsage, name)
		}
	}
	return values, nil
}

func (v flagValues) one(name string) string {
	if len(v[name]) == 0 {
		return ""
	}
	return v[name][0]
}
func (v flagValues) all(name string) []string { return append([]string(nil), v[name]...) }
func (v flagValues) int64(name string) (int64, error) {
	n, err := strconv.ParseInt(v.one(name), 10, 64)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%w: %s must be a positive integer", ErrUsage, name)
	}
	return n, nil
}

func decodeInputFile[T any](path string) (T, error) {
	var result T
	info, err := os.Lstat(path)
	if err != nil {
		return result, fmt.Errorf("%w: input file: %v", ErrUsage, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return result, fmt.Errorf("%w: input file must be a regular non-symlink", ErrUsage)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return result, fmt.Errorf("%w: input file: %v", ErrUsage, err)
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() {
		return result, fmt.Errorf("%w: input file must remain regular", ErrUsage)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return result, fmt.Errorf("%w: input file: %v", ErrUsage, err)
	}
	if len(data) > maxInputBytes || !utf8.Valid(data) {
		return result, fmt.Errorf("%w: input file must be valid UTF-8 JSON", ErrUsage)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return result, fmt.Errorf("%w: invalid input JSON: %v", ErrUsage, err)
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return result, fmt.Errorf("%w: input file must contain one JSON document", ErrUsage)
	}
	return result, nil
}
