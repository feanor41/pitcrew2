package envelope

import (
	"encoding/json"
	"io"
)

type ExitCode int

const (
	OK ExitCode = iota
	Internal
	Usage
	State
	CAS
	Handle
)

var codeNames = [...]string{"ok", "internal", "usage", "state", "cas", "handle"}

type Response struct {
	OK         bool     `json:"ok"`
	Data       any      `json:"data"`
	Warnings   []string `json:"warnings"`
	NextAction string   `json:"next_action"`
}

type Failure struct {
	OK    bool        `json:"ok"`
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func WriteSuccess(w io.Writer, data any, warnings []string, nextAction string) error {
	if warnings == nil {
		warnings = []string{}
	}
	return json.NewEncoder(w).Encode(Response{OK: true, Data: data, Warnings: warnings, NextAction: nextAction})
}

func WriteFailure(w io.Writer, code ExitCode, message string) error {
	name := "internal"
	if code >= OK && int(code) < len(codeNames) {
		name = codeNames[code]
	}
	return json.NewEncoder(w).Encode(Failure{Error: ErrorDetail{Code: name, Message: message}})
}
