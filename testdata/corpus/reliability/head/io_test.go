package drain

import (
	"errors"
	"testing"
)

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

func TestClose(t *testing.T) {
	if err := Close(closerFunc(func() error { return nil })); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
	if err := Close(closerFunc(func() error { return errors.New("boom") })); err == nil {
		t.Fatal("Close() swallowed the close error")
	}
}
