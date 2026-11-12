package terminal

import (
	"slices"
	"testing"
)

func TestHostTerminalVariablesAreNotInherited(t *testing.T) {
	env := hostTerminalFree([]string{"PATH=/bin", "TERM_PROGRAM=Apple_Terminal", "TERM_PROGRAM_VERSION=1", "TERM_SESSION_ID=x", "TERM=dumb"})
	if !slices.Equal(env, []string{"PATH=/bin", "TERM=dumb"}) {
		t.Fatal(env)
	}
}
