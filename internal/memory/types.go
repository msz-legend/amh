// Package memory defines the domain types of AgentMemory Hub:
// memory kinds, lifecycle status, trust levels, and the Memory record itself.
package memory

import (
	"errors"
	"fmt"
	"time"
)

// Type enumerates the six MVP memory kinds (PRD §6 M1).
type Type string

const (
	TypeDecision    Type = "decision"
	TypeGotcha      Type = "gotcha"
	TypePreference  Type = "preference"
	TypeProjectRule Type = "project-rule"
	TypeHandoff     Type = "handoff"
	TypeNote        Type = "note"
)

func (t Type) Valid() bool {
	switch t {
	case TypeDecision, TypeGotcha, TypePreference, TypeProjectRule, TypeHandoff, TypeNote:
		return true
	}
	return false
}

// Status is the lifecycle of a memory.
type Status string

const (
	StatusActive   Status = "active"   // served to agents
	StatusProposed Status = "proposed" // awaiting human review
	StatusRetired  Status = "retired"  // superseded or forgotten, kept as history
)

func (s Status) Valid() bool {
	switch s {
	case StatusActive, StatusProposed, StatusRetired:
		return true
	}
	return false
}

// Trust grades how a memory entered the store.
type Trust string

const (
	// TrustExplicit: an agent called remember with this content directly.
	TrustExplicit Trust = "explicit"
	// TrustExtracted: produced by the extraction worker from a session summary.
	TrustExtracted Trust = "extracted"
	// TrustUser: approved by the human via review.
	TrustUser Trust = "user"
)

func (t Trust) Valid() bool {
	switch t {
	case TrustExplicit, TrustExtracted, TrustUser:
		return true
	}
	return false
}

// Memory is the core record. Immutable once active except for status/superseded_by.
type Memory struct {
	ID           string
	Type         Type
	Content      string
	ProjectKey   string // normalized git remote or escaped cwd fallback
	SourceAgent  string // "claude-code", "codex", ... — decided by client config, never model self-report
	Trust        Trust
	Status       Status
	SupersededBy string // empty unless retired via supersede/forget
	FirstSeen    time.Time
	CreatedAt    time.Time
}

var (
	ErrEmptyContent = errors.New("memory: content must not be empty")
	ErrInvalidType  = errors.New("memory: invalid type")
	ErrInvalidTrust = errors.New("memory: invalid trust")
)

// Normalize validates and fills defaults. status defaults to active for
// explicit writes; extracted/proposed handling lives in the store layer.
func (m *Memory) Normalize(now time.Time) error {
	if m.Content == "" {
		return ErrEmptyContent
	}
	if !m.Type.Valid() {
		if m.Type == "" {
			m.Type = TypeNote
		} else {
			return fmt.Errorf("%w: %q", ErrInvalidType, string(m.Type))
		}
	}
	if m.Trust == "" {
		m.Trust = TrustExplicit
	}
	if !m.Trust.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidTrust, string(m.Trust))
	}
	if m.FirstSeen.IsZero() {
		m.FirstSeen = now
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	return nil
}
