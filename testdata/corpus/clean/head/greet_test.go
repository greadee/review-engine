package greet

import "testing"

func TestGreet(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"", "hello"},
		{"Ada", "hello Ada"},
	}
	for _, tc := range cases {
		if got := Greet(tc.name); got != tc.want {
			t.Fatalf("Greet(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}
