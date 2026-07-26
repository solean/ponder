package model

// ArenaTurnToFullTurn converts Arena's per-player turn ordinal into the
// user-facing full-turn number shared by both players. Arena turns 1 and 2
// are full turn 1, turns 3 and 4 are full turn 2, and so on. Non-positive
// values are preserved for callers that use zero as an unknown/pre-game
// sentinel.
func ArenaTurnToFullTurn(arenaTurn int64) int64 {
	if arenaTurn <= 0 {
		return arenaTurn
	}
	return (arenaTurn + 1) / 2
}

// ArenaTurnPtrToFullTurn is the nullable counterpart to ArenaTurnToFullTurn.
// It returns a fresh pointer so projecting an API value never mutates a raw
// Arena turn retained elsewhere.
func ArenaTurnPtrToFullTurn(arenaTurn *int64) *int64 {
	if arenaTurn == nil {
		return nil
	}
	fullTurn := ArenaTurnToFullTurn(*arenaTurn)
	return &fullTurn
}
