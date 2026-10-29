package main

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestStateAccessErrorExplainsAdministratorOnly(t *testing.T) {
	denied := fmt.Errorf("agentcfg: read config: %w", fs.ErrPermission)
	err := stateAccessError(`C:\ProgramData\ai-usage-agent`, denied)
	if !errors.Is(err, fs.ErrPermission) || !strings.Contains(err.Error(), "administrator") {
		t.Fatalf("err = %v, want the permission error with an administrator hint", err)
	}
	other := errors.New("bad json")
	if got := stateAccessError("dir", other); got != other {
		t.Fatalf("other errors must pass through unchanged, got %v", got)
	}
	if stateAccessError("dir", nil) != nil {
		t.Fatal("nil must stay nil")
	}
}
