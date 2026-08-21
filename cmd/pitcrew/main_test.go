package main

import (
	"bytes"
	"testing"
)

func TestRunDelegatesGlobalVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--version"}, &stdout, &stderr, t.TempDir()); code != 0 || stdout.String() != "pitcrew dev\n" || stderr.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
