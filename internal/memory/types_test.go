package memory

import (
	"errors"
	"testing"
	"time"
)

func TestNormalizeDefaults(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	m := &Memory{Content: "use pnpm, not npm", Type: TypeProjectRule, SourceAgent: "claude-code"}
	if err := m.Normalize(now); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if m.Trust != TrustExplicit {
		t.Errorf("default trust = %q, want explicit", m.Trust)
	}
	if m.Status != StatusActive && m.Status != "" {
		t.Errorf("unexpected status %q", m.Status)
	}
	if m.FirstSeen != now || m.CreatedAt != now {
		t.Errorf("timestamps not defaulted")
	}
}

func TestNormalizeEmptyTypeBecomesNote(t *testing.T) {
	m := &Memory{Content: "x"}
	if err := m.Normalize(time.Now()); err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if m.Type != TypeNote {
		t.Errorf("empty type = %q, want note", m.Type)
	}
}

func TestNormalizeRejectsBad(t *testing.T) {
	now := time.Now()
	if err := (&Memory{Content: ""}).Normalize(now); !errors.Is(err, ErrEmptyContent) {
		t.Errorf("empty content: got %v", err)
	}
	if err := (&Memory{Content: "x", Type: Type("bogus")}).Normalize(now); !errors.Is(err, ErrInvalidType) {
		t.Errorf("bad type: got %v", err)
	}
	if err := (&Memory{Content: "x", Trust: Trust("bogus")}).Normalize(now); !errors.Is(err, ErrInvalidTrust) {
		t.Errorf("bad trust: got %v", err)
	}
}

func TestTypeAndStatusValidity(t *testing.T) {
	for _, ty := range []Type{TypeDecision, TypeGotcha, TypePreference, TypeProjectRule, TypeHandoff, TypeNote} {
		if !ty.Valid() {
			t.Errorf("type %q should be valid", ty)
		}
	}
	if Type("mood").Valid() {
		t.Error("bogus type reported valid")
	}
	for _, s := range []Status{StatusActive, StatusProposed, StatusRetired} {
		if !s.Valid() {
			t.Errorf("status %q should be valid", s)
		}
	}
}
