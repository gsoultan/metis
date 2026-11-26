package grule

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// warnings are what the report says about the import as a whole.
func (t *translation) warnings(groups []group) []string {
	var out []string
	for _, g := range groups {
		if tie := salienceTie(g); tie != "" {
			out = append(out, tie)
		}
	}
	out = append(out, t.earlyReads(groups)...)
	return out
}

// salienceTie names rules of one table with the same salience. Grule does not
// promise which of them fires first; the table keeps them in file order.
func salienceTie(g group) string {
	bySalience := map[int][]string{}
	for _, r := range slices.SortedFunc(slices.Values(g.rules), func(a, b *analysedRule) int { return a.order - b.order }) {
		bySalience[r.Salience] = append(bySalience[r.Salience], r.Name)
	}
	var ties []string
	for salience, names := range bySalience {
		if len(names) > 1 {
			ties = append(ties, fmt.Sprintf("%s (salience %d)", strings.Join(names, ", "), salience))
		}
	}
	if len(ties) == 0 {
		return ""
	}
	sort.Strings(ties)
	return fmt.Sprintf("%s: rules with equal salience are kept in the order they were written; Grule does not promise that order: %s",
		strings.Join(g.fields, ", "), strings.Join(ties, "; "))
}

// earlyReads warns about a rule that reads a field more than one rule sets.
// Grule may fire the reader as soon as the first of them has fired; metis
// decides it after all of them.
func (t *translation) earlyReads(groups []group) []string {
	writers := map[string]int{}
	for _, g := range groups {
		for _, field := range g.fields {
			writers[field] += len(g.rules)
		}
	}
	var out []string
	for _, g := range groups {
		for _, r := range g.rules {
			for _, key := range sortedKeys(r.reads) {
				if writers[key] > 1 && !r.writesField(key) {
					out = append(out, fmt.Sprintf("%s reads %s, which %d rules set; Grule may fire %s before the last of them, metis decides it after all of them",
						r.Name, key, writers[key], r.Name))
				}
			}
		}
	}
	return out
}
