// Package eval provides the E1 retrieval benchmark: a deterministic
// synthetic corpus (100 memories over 20 topics) and 30 queries with
// topic-membership ground truth, scored as mean recall@5. Threshold: 0.85.
package eval

import (
	"fmt"
	"math/rand"
	"time"

	"amh/internal/memory"
	"amh/internal/store"
)

// Threshold is the PRD acceptance bar for mean recall@5.
const Threshold = 0.85

// Miss is one query that failed to retrieve its full relevant set.
type Miss struct {
	Query  string
	Recall float64 // |top5 ∩ relevant| / |relevant|
}

// Result summarizes one benchmark run.
type Result struct {
	Memories  int
	Queries   int
	RecallAt5 float64
	Misses    []Miss
}

func (r *Result) Pass() bool { return r.RecallAt5 >= Threshold }

// topics: 20 keyword pairs; every corpus memory contains both keywords of
// its topic, so ground truth (same-topic membership) is unambiguous.
var topics = [][2]string{
	{"sqlite", "migration"}, {"mcp", "stdio"}, {"fts5", "tokenize"},
	{"recency", "bonus"}, {"secrets", "detection"}, {"mirror", "card"},
	{"spool", "session"}, {"review", "approval"}, {"remote", "origin"},
	{"hook", "injection"}, {"compose", "volume"}, {"oauth", "consent"},
	{"cache", "invalidation"}, {"postgres", "vacuum"}, {"goroutine", "leak"},
	{"bundler", "treeshake"}, {"yaml", "anchors"}, {"kubectl", "rollout"},
	{"websocket", "backoff"}, {"protobuf", "wireformat"},
}

// fillers pad the variant sentences; none collides with any keyword above.
var fillers = []string{
	"today", "deploy", "fixup", "notebook", "revisit", "tempfile", "upgrade",
	"team", "decided", "infra", "pipeline", "runtime", "batch", "audit",
	"checkpoint", "rollback", "nightly", "verbose", "corner", "landing",
}

// corpusVariant builds variant j of topic i deterministically.
func corpusVariant(rng *rand.Rand, i, j int) string {
	t := topics[i]
	pick := func() string { return fillers[rng.Intn(len(fillers))] }
	return fmt.Sprintf("%s %s: %s and %s rely on %s %s handling (%s variant %d)",
		t[0], t[1], pick(), pick(), t[0], t[1], t[0], j)
}

// Run builds the corpus in db, runs the 30 benchmark queries through
// store.Search and scores mean recall@5.
func Run(db *store.DB) (*Result, error) {
	rng := rand.New(rand.NewSource(42)) // deterministic corpus
	base := time.Now()
	ids := make([][]string, len(topics)) // relevant sets per topic
	n := 0
	for i := range topics {
		for j := 0; j < 5; j++ {
			m := &memory.Memory{
				Type:      memory.TypeNote,
				Content:   corpusVariant(rng, i, j),
				CreatedAt: base.Add(time.Duration(n) * time.Minute),
			}
			if err := db.Insert(m); err != nil {
				return nil, fmt.Errorf("eval: insert %d: %w", n, err)
			}
			ids[i] = append(ids[i], m.ID)
			n++
		}
	}
	res := &Result{Memories: n, Queries: len(topics) + 10}
	var sum float64
	for q := 0; q < res.Queries; q++ {
		i := q % len(topics)
		query := topics[i][0] + " " + topics[i][1]
		if q%2 == 1 { // vary term order
			query = topics[i][1] + " " + topics[i][0]
		}
		hits, err := db.Search(query, "", 5)
		if err != nil {
			return nil, fmt.Errorf("eval: query %q: %w", query, err)
		}
		relevant := map[string]bool{}
		for _, id := range ids[i] {
			relevant[id] = true
		}
		found := 0
		for _, h := range hits {
			if relevant[h.ID] {
				found++
			}
		}
		recall := float64(found) / float64(len(ids[i]))
		sum += recall
		if recall < 1 {
			res.Misses = append(res.Misses, Miss{Query: query, Recall: recall})
		}
	}
	res.RecallAt5 = sum / float64(res.Queries)
	return res, nil
}
