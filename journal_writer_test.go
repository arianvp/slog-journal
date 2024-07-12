package slogjournal

import (
	"testing"
)

func TestJournalWriter(t *testing.T) {
	_, err := newJournalWriter("")
	if err != nil {
		t.Fatal(err)
	}
}
