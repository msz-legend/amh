// Package consolidate implements F1: rule-based duplicate and contradiction
// detection over active memories. It ONLY produces a proposal report —
// it never mutates store data. Suggested actions reference amh review /
// supersede so a human stays in the loop.
package consolidate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"amh/internal/memory"
	"amh/internal/store"
)

// Suggestion is one proposed action in the report.
type Suggestion struct {
	Kind       string   // "duplicate" | "contradiction"
	KeepID     string   // recommended survivor (newest)
	RetireIDs  []string // candidates to retire via supersede
	Detail     string
}

// Report is the full proposal output.
type Report struct {
	Suggestions []Suggestion
	Scanned     int
	GeneratedAt time.Time
}

// Render writes the human-readable Markdown report.
func (r *Report) Render() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# amh consolidate proposals (%s)\n\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "Scanned %d active memories. Proposal only — nothing was changed.\n", r.Scanned)
	fmt.Fprintf(&b, "Apply with: `amh review reject <id>` (retires the memory) or the web UI.\n\n")
	if len(r.Suggestions) == 0 {
		b.WriteString("_No duplicates or contradictions detected._\n")
		return b.String()
	}
	for i, s := range r.Suggestions {
		fmt.Fprintf(&b, "## %d. %s — keep %s\n", i+1, s.Kind, short(s.KeepID))
		b.WriteString(s.Detail)
		fmt.Fprintf(&b, "\n\nRetire candidates: %s\n", joinShort(s.RetireIDs))
	}
	return b.String()
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func joinShort(ids []string) string {
	var out []string
	for _, id := range ids {
		out = append(out, "`"+short(id)+"`")
	}
	return strings.Join(out, ", ")
}

const dupJaccard = 0.8 // token-set overlap treated as duplicate

var negations = map[string]bool{"not": true, "dont": true, "don't": true,
	"never": true, "avoid": true, "do-not": true}

// Run scans active memories and returns rule-based proposals:
//   - duplicates: same project+type with token-set Jaccard >= 0.8
//   - contradictions: near-identical token sets within same project+type
//     where exactly one side is negated ("don't/never/avoid ...")
func Run(db *store.DB) (*Report, error) {
	mems, err := db.List(memory.StatusActive, "", 0)
	if err != nil {
		return nil, err
	}
	rep := &Report{Scanned: len(mems), GeneratedAt: time.Now()}
	type entry struct {
		m    *memory.Memory
		toks map[string]bool
		neg  bool
	}
	entries := make([]entry, 0, len(mems))
	for _, m := range mems {
		toks := tokenize(m.Content)
		if len(toks) == 0 {
			continue
		}
		neg := false
		for t := range toks {
			if negations[t] {
				neg = true
				break
			}
		}
		entries = append(entries, entry{m: m, toks: toks, neg: neg})
	}
	// Deterministic scan order: oldest first so KeepID ends up newest.
	sort.Slice(entries, func(a, b int) bool {
		return entries[a].m.CreatedAt.Before(entries[b].m.CreatedAt)
	})
	for i := 0; i < len(entries); i++ {
		for j := i + 1; j < len(entries); j++ {
			a, b := entries[i], entries[j]
			if a.m.ProjectKey != b.m.ProjectKey || a.m.Type != b.m.Type {
				continue
			}
			jac := jaccard(a.toks, b.toks)
			if jac < dupJaccard {
				continue
			}
			kind := ""
			detail := ""
			if a.neg != b.neg {
				kind = "contradiction"
				detail = fmt.Sprintf("Near-identical statements (overlap %.2f) with opposing polarity:\n- keep %s: %q\n- retire %s: %q\n",
					jac, short(a.m.ID), a.m.Content, short(b.m.ID), b.m.Content)
				rep.add(Suggestion{Kind: kind, KeepID: b.m.ID, RetireIDs: []string{a.m.ID}, Detail: detail})
				continue
			}
			kind = "duplicate"
			detail = fmt.Sprintf("Overlap %.2f within %s/%s:\n- keep %s: %q\n- retire %s: %q\n",
				jac, a.m.ProjectKey, a.m.Type, short(b.m.ID), b.m.Content, short(a.m.ID), a.m.Content)
			rep.add(Suggestion{Kind: kind, KeepID: b.m.ID, RetireIDs: []string{a.m.ID}, Detail: detail})
		}
	}
	return rep, nil
}

func (r *Report) add(s Suggestion) {
	// Merge into an existing duplicate group when the keep target matches.
	for i := range r.Suggestions {
		if r.Suggestions[i].Kind == s.Kind && r.Suggestions[i].KeepID == s.KeepID {
			r.Suggestions[i].RetireIDs = append(r.Suggestions[i].RetireIDs, s.RetireIDs...)
			r.Suggestions[i].Detail += s.Detail
			return
		}
	}
	r.Suggestions = append(r.Suggestions, s)
}

func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	}) {
		out[f] = true
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	inter := 0
	for t := range a {
		if b[t] {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}
