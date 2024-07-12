package slogjournal

import (
	"context"
	"log/slog"
	"testing"
)

func TestCanWriteMessageToJournal(t *testing.T) {
	handler, err := NewHandler(nil)
	if err != nil {
		t.Fatal("Error creating new handler")
	}

	if err := handler.Handle(context.TODO(), slog.Record{Level: slog.LevelInfo, Message: "Hello, World!"}); err != nil {
		t.Fatal(err)
	}
}
