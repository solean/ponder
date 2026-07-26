package model

import "testing"

func TestArenaTurnToFullTurn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arenaTurn int64
		want      int64
	}{
		{name: "negative sentinel", arenaTurn: -1, want: -1},
		{name: "zero sentinel", arenaTurn: 0, want: 0},
		{name: "first players first turn", arenaTurn: 1, want: 1},
		{name: "second players first turn", arenaTurn: 2, want: 1},
		{name: "first players second turn", arenaTurn: 3, want: 2},
		{name: "second players second turn", arenaTurn: 4, want: 2},
		{name: "later odd turn", arenaTurn: 19, want: 10},
		{name: "later even turn", arenaTurn: 20, want: 10},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ArenaTurnToFullTurn(test.arenaTurn); got != test.want {
				t.Fatalf("ArenaTurnToFullTurn(%d) = %d, want %d", test.arenaTurn, got, test.want)
			}
		})
	}
}

func TestArenaTurnPtrToFullTurn(t *testing.T) {
	t.Parallel()

	if got := ArenaTurnPtrToFullTurn(nil); got != nil {
		t.Fatalf("ArenaTurnPtrToFullTurn(nil) = %v, want nil", got)
	}

	raw := int64(8)
	got := ArenaTurnPtrToFullTurn(&raw)
	if got == nil || *got != 4 {
		t.Fatalf("ArenaTurnPtrToFullTurn(8) = %v, want 4", got)
	}
	if got == &raw {
		t.Fatal("ArenaTurnPtrToFullTurn returned the raw value's pointer")
	}
	if raw != 8 {
		t.Fatalf("raw turn mutated to %d", raw)
	}
}
