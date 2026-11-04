package spec

import (
	"sort"
	"strings"
	"unicode"
)

// Result is one operation matched by Search.
type Result struct {
	Operation Operation
	// Score orders results: higher is better. It grows with the number of
	// query words matched first, then with how strongly they matched.
	Score float64
}

// Field weights: a query word scores the best weight among the fields it
// appears in.
const (
	weightID          = 4
	weightSummary     = 3
	weightTag         = 3
	weightPath        = 2
	weightParam       = 1.5
	weightDescription = 1
	// weightNotes is for the overlay's curated usage notes, which are written
	// to answer task-shaped queries ("follow-ups are tickets"), so they count
	// more than the spec's generic descriptions.
	weightNotes = 2

	// weightExactName and weightExactSummary reward a query that is exactly
	// an operation's name ("create change" for create_change) or summary.
	// Both are below the 100 per matched word, so they only reorder
	// operations matching every word. A name match beats a summary match:
	// "list service environments" is list_service_environments, even though
	// list_environment_services's summary has the same words.
	weightExactName    = 50
	weightExactSummary = 40
	// weightOrder rewards each adjacent query word pair that appears in the
	// same order in the summary or name, separating list_service_environments
	// from list_environment_services. Small enough to only break ties.
	weightOrder = 0.1

	// weightSynonym scales a field's weight when a query word matches it
	// through a synonym rather than literally.
	weightSynonym = 0.5
)

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "for": true, "to": true, "of": true,
	"and": true, "or": true, "in": true, "on": true, "by": true, "with": true,
}

// Search ranks operations against a free-text query and returns at most
// limit results (all of them when limit <= 0). Operations matching more of
// the query's words rank first, then by match strength, an exact name or
// summary match, how much of the summary and name the query covers, and word
// order, so "Get severity matrix" outranks "Delete a severity matrix
// condition" for "severity matrix".
func (s *Spec) Search(query string, limit int) []Result {
	sequence := tokenize(query)
	words := uniq(sequence)
	if len(words) == 0 {
		return nil
	}
	exactName := strings.Join(rawWords(query), "_")
	exactSummary := strings.Join(sequence, " ")
	queryWords := make(map[string]bool, len(words))
	for _, w := range words {
		queryWords[w] = true
	}

	type ranked struct {
		Result
		index int
	}
	var hits []ranked
	for i, op := range s.Operations {
		fields := searchFields(op)
		matched, strength := 0, 0.0
		for _, w := range words {
			best := 0.0
			for _, f := range fields {
				if f.weight > best && f.words[w] {
					best = f.weight
				}
				// A synonym counts as a match at reduced weight, so the
				// word the user typed always beats its synonyms.
				for _, alt := range synonyms[w] {
					if f.weight*weightSynonym > best && f.words[alt] {
						best = f.weight * weightSynonym
					}
				}
			}
			if best > 0 {
				matched++
				strength += best
			}
		}
		if matched == 0 {
			continue
		}
		summary, name := tokenize(op.Summary), tokenize(op.ID)
		// Words matched dominate; then match strength; then an exact name or
		// summary match; then how much of the summary and name the query
		// covers; then word order.
		score := float64(matched)*100 + strength
		switch {
		case exactName == op.ID:
			score += weightExactName
		case exactSummary == strings.Join(summary, " "):
			score += weightExactSummary
		}
		score += (coverage(summary, queryWords) + coverage(name, queryWords)) / 2
		score += weightOrder * float64(max(pairsInOrder(sequence, summary), pairsInOrder(sequence, name)))
		hits = append(hits, ranked{Result{Operation: op, Score: score}, i})
	}

	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.index < b.index
	})

	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Result, len(hits))
	for i, h := range hits {
		out[i] = h.Result
	}
	return out
}

type searchField struct {
	weight float64
	words  map[string]bool
}

func searchFields(op Operation) []searchField {
	var params, details []string
	for _, group := range [][]Param{op.PathParams, op.QueryParams, op.BodyFields} {
		for _, p := range group {
			params = append(params, p.Name)
			params = append(params, p.Enum...)
			details = append(details, p.Description)
		}
	}
	return []searchField{
		{weightID, wordSet(op.ID)},
		{weightSummary, wordSet(op.Summary)},
		{weightTag, wordSet(op.Tag)},
		{weightPath, wordSet(op.Path)},
		{weightParam, wordSet(strings.Join(params, " "))},
		{weightNotes, wordSet(strings.Join(op.Notes, " "))},
		{weightDescription, wordSet(op.Description + " " + strings.Join(details, " "))},
	}
}

// synonyms maps a query word (after tokenizing) to words that mean the same
// thing in FireHydrant's API, matched at weightSynonym. Keep this short:
// verbs responders use for CRUD, plus domain terms whose API name differs
// (follow-ups are tickets; postmortems are retrospectives).
var synonyms = map[string][]string{
	"add":        {"create"},
	"new":        {"create"},
	"remove":     {"delete"},
	"archive":    {"delete"},
	"edit":       {"update"},
	"show":       {"get"},
	"fetch":      {"get"},
	"followup":   {"follow", "ticket"},
	"postmortem": {"retrospective", "post", "mortem"},
	"retro":      {"retrospective"},
	"rca":        {"retrospective"},
	"oncall":     {"call"},
	"milestone":  {"lifecycle", "phase"},
}

func wordSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, w := range tokenize(s) {
		set[w] = true
	}
	return set
}

// coverage is the fraction of distinct summary words that are query words.
func coverage(summary []string, query map[string]bool) float64 {
	distinct := uniq(summary)
	if len(distinct) == 0 {
		return 0
	}
	n := 0
	for _, w := range distinct {
		if query[w] {
			n++
		}
	}
	return float64(n) / float64(len(distinct))
}

// pairsInOrder counts adjacent pairs in query that are also adjacent, in the
// same order, in text.
func pairsInOrder(query, text []string) int {
	pairs := map[[2]string]bool{}
	for i := 1; i < len(text); i++ {
		pairs[[2]string{text[i-1], text[i]}] = true
	}
	n := 0
	for i := 1; i < len(query); i++ {
		if pairs[[2]string{query[i-1], query[i]}] {
			n++
		}
	}
	return n
}

// rawWords lowercases s and splits it on anything that isn't a letter or
// digit, keeping every word as written (no stop words dropped, no
// singularizing), for exact-name comparison.
func rawWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// tokenize lowercases s, splits it on anything that isn't a letter or digit
// (so snake_case, kebab-case, paths and {params} break apart), drops stop
// words, and singularizes each word.
func tokenize(s string) []string {
	fields := rawWords(s)
	words := fields[:0]
	for _, f := range fields {
		if stopWords[f] {
			continue
		}
		words = append(words, singularize(f))
	}
	return words
}

// singularize strips simple English plurals: "policies" → "policy",
// "pages" → "page". Words ending in "ss", "us" or "is" are left alone
// ("process", "status", "analysis").
func singularize(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 3 && strings.HasSuffix(w, "s") &&
		!strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		return w[:len(w)-1]
	}
	return w
}

func uniq(words []string) []string {
	seen := make(map[string]bool, len(words))
	out := words[:0:0]
	for _, w := range words {
		if !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}
