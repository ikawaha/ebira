package main

import (
	"errors"
	"fmt"
	"os"
	"testing"

	goa "goa.design/goa/v3/pkg"

	gennotes "github.com/ikawaha/ebira/gen/notes"
)

func TestMCPError(t *testing.T) {
	declared := gennotes.MakeNotFound(errors.New("note not found"))
	invalid := goa.InvalidLengthError("body.q", "x", 1001, 1000, false)
	for name, tc := range map[string]struct {
		err  error
		kept bool
	}{
		"declared":         {declared, true},
		"declared wrapped": {fmt.Errorf("get: %w", declared), true},
		"invalid":          {invalid, true},
		"file system":      {&os.PathError{Op: "open", Path: "/Users/me/notes/x.yaml", Err: os.ErrPermission}, false},
		"fault":            {goa.Fault("index broke at /Users/me/.cache/ebira"), false},
	} {
		got := mcpError(tc.err)
		if tc.kept && got != tc.err {
			t.Errorf("%s: mcpError = %v, want the error itself", name, got)
		}
		if !tc.kept && got != errMCPFailed {
			t.Errorf("%s: mcpError = %v, want errMCPFailed", name, got)
		}
	}
}
