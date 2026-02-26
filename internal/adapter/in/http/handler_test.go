package http

import "testing"

func TestResolveRequestedID(t *testing.T) {
	t.Parallel()

	t.Run("numeric id", func(t *testing.T) {
		t.Parallel()
		got, err := resolveRequestedID("123", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "123" {
			t.Fatalf("expected 123, got %s", got)
		}
	})

	t.Run("md5 id", func(t *testing.T) {
		t.Parallel()
		got, err := resolveRequestedID("0123456789abcdef0123456789ABCDEF", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got == "" {
			t.Fatal("expected non-empty id")
		}
	})

	t.Run("dokId and fileId", func(t *testing.T) {
		t.Parallel()
		got, err := resolveRequestedID("", "12", "34")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "12/34" {
			t.Fatalf("expected 12/34, got %s", got)
		}
	})

	t.Run("invalid id", func(t *testing.T) {
		t.Parallel()
		_, err := resolveRequestedID("abc", "", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
