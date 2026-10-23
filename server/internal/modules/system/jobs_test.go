package system

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegisterRejectsEmptyAndDuplicateKinds(t *testing.T) {
	jobs := NewJobs(nil)
	noop := func(context.Context, json.RawMessage) error { return nil }

	if err := jobs.Register("identity.email_delivery", noop); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	for name, register := range map[string]func() error{
		"empty kind":     func() error { return jobs.Register("", noop) },
		"nil handler":    func() error { return jobs.Register("identity.other", nil) },
		"duplicate kind": func() error { return jobs.Register("identity.email_delivery", noop) },
		"built-in kind":  func() error { return jobs.Register("system.noop", noop) },
	} {
		if err := register(); err == nil {
			t.Fatalf("%s was accepted", name)
		}
	}
	if len(jobs.handlers) != 2 {
		t.Fatalf("registry holds %d kinds, want the built-in noop plus one registration", len(jobs.handlers))
	}
}
