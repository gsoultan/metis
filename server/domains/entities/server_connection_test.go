package entities

import (
	"strings"
	"testing"
)

func TestThereAreTwoHundredFiftySixDistinctConnectionWords(t *testing.T) {
	seen := map[string]bool{}
	for _, w := range connectionWords {
		if seen[w] {
			t.Errorf("%q is listed twice", w)
		}
		seen[w] = true
	}
	if len(connectionWords) != 256 {
		t.Fatalf("there are %d connection words; four of them carry 32 bits only with 256", len(connectionWords))
	}
}

func TestBothServersShowTheSameWordsAndAnotherKeyShowsOthers(t *testing.T) {
	words := ConnectionWords("K7QX-9MFD", "up", "down")
	if got := ConnectionWords("K7QX-9MFD", "up", "down"); got != words {
		t.Fatalf("the same code and keys gave %q and %q", words, got)
	}
	if len(strings.Fields(words)) != 4 {
		t.Fatalf("%q is not four words", words)
	}
	if ConnectionWords("K7QX-9MFD", "up", "someone else") == words {
		t.Fatal("a swapped key showed the same words")
	}
}
