package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAppendAndHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	thread := "thread-1"
	if err := st.Append(ctx, thread, "user", "hi"); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(ctx, thread, "assistant", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := st.Append(ctx, "other", "user", "nope"); err != nil {
		t.Fatal(err)
	}

	hist, err := st.History(ctx, thread, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("len=%d", len(hist))
	}
	if hist[0].Role != "user" || hist[0].Content != "hi" {
		t.Fatalf("%#v", hist[0])
	}
	if hist[1].Role != "assistant" || hist[1].Content != "hello" {
		t.Fatalf("%#v", hist[1])
	}

	n, err := st.Count(ctx, thread)
	if err != nil || n != 2 {
		t.Fatalf("count=%d err=%v", n, err)
	}

	if err := st.Clear(ctx, thread); err != nil {
		t.Fatal(err)
	}
	hist, err = st.History(ctx, thread, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 0 {
		t.Fatalf("expected empty after clear, got %#v", hist)
	}
}

func TestHistoryLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lim.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := st.Append(ctx, "t", "user", string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	hist, err := st.History(ctx, "t", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) != 2 {
		t.Fatalf("len=%d", len(hist))
	}
	if hist[0].Content != "d" || hist[1].Content != "e" {
		t.Fatalf("expected last two, got %#v", hist)
	}
}
