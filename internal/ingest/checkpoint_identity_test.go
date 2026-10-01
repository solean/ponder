package ingest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func checkpointCompletedMatch(id, self string) string {
	return fmt.Sprintf(`{"timestamp":"1773367612385","matchGameRoomStateChangedEvent":{"gameRoomInfo":{"gameRoomConfig":{"matchId":%q,"reservedPlayers":[{"userId":"opp","playerName":"Opponent","systemSeatId":1,"teamId":1},{"userId":%q,"playerName":"Self","systemSeatId":2,"teamId":2}]},"stateType":"MatchGameRoomStateType_MatchCompleted","finalMatchResult":{"matchId":%q,"resultList":[{"scope":"MatchScope_Match","winningTeamId":1}]}}}}`, id, self, id)
}

func TestRotationDoesNotConsumeNewerPendingRank(t *testing.T) {
	for _, replacePrevious := range []bool{false, true} {
		t.Run(fmt.Sprintf("replace_previous_%v", replacePrevious), func(t *testing.T) {
			ctx := context.Background()
			database, store, current := checkpointDatabase(t)
			previous := filepath.Join(filepath.Dir(current), "Player-prev.log")
			if replacePrevious {
				if err := writeLogLines(previous, []string{`{"PersonaId":"self"}`, "old session"}, false); err != nil {
					t.Fatal(err)
				}
				if _, err := NewParser(store).ParseFile(ctx, previous, true); err != nil {
					t.Fatal(err)
				}
			}
			lines := []string{`{"PersonaId":"self"}`, checkpointCompletedMatch("rank-a", "self"), `<== RankGetCombinedRankInfo(old-rank)`, `{"constructedLevel":3}`, checkpointCompletedMatch("rank-b", "self")}
			if err := writeLogLines(current, lines, false); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
				t.Fatal(err)
			}
			// Arena rotates Player.log to Player-prev.log; live startup imports it.
			if err := os.Rename(current, previous); err != nil {
				t.Fatal(err)
			}
			if stats, err := NewParser(store).ParseFile(ctx, previous, true); err != nil {
				t.Fatal(err)
			} else if stats.LinesRead != 0 {
				t.Fatalf("rotated log replayed %d old lines", stats.LinesRead)
			}
			var count int
			if err := database.QueryRow(`SELECT count(*) FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='rank-b'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("old rank response was assigned to newer pending match B after rotation")
			}
			// The cursor must be saved under the new path before the original path is
			// reused. Its pending rank still belongs to B when the next response arrives.
			if err := writeLogLines(current, []string{`<== RankGetCombinedRankInfo(new-rank)`, `{"constructedLevel":9}`}, false); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
				t.Fatal(err)
			}
			if stats, err := NewParser(store).ParseFile(ctx, previous, true); err != nil {
				t.Fatal(err)
			} else if stats.LinesRead != 0 {
				t.Fatalf("second restart replayed %d old lines", stats.LinesRead)
			}
			for id, want := range map[string]int{"rank-a": 3, "rank-b": 9} {
				var level int
				if err := database.QueryRow(`SELECT constructed_level FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id=?`, id).Scan(&level); err != nil {
					t.Fatal(err)
				}
				if level != want {
					t.Fatalf("%s rank=%d, want %d", id, level, want)
				}
			}
		})
	}
}

func TestRotationRestoresContextForUnparsedSuffix(t *testing.T) {
	ctx := context.Background()
	control, store, logPath := checkpointDatabase(t)
	lines := checkpointFixture()
	if err := writeLogLines(logPath, lines, false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, logPath, true); err != nil {
		t.Fatal(err)
	}
	want := checkpointRows(t, control)

	database, store, current := checkpointDatabase(t)
	if err := writeLogLines(current, lines[:3], false); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(current, lines[3:], true); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(filepath.Dir(current), "Player-prev.log")
	if err := os.Rename(current, previous); err != nil {
		t.Fatal(err)
	}
	if stats, err := NewParser(store).ParseFile(ctx, previous, true); err != nil {
		t.Fatal(err)
	} else if stats.LinesRead != 2 {
		t.Fatalf("rotated log read %d lines, want 2 new lines", stats.LinesRead)
	}
	if got := checkpointRows(t, database); !reflect.DeepEqual(got, want) {
		t.Fatalf("rotation observations differ\ngot: %#v\nwant: %#v", got, want)
	}
}

func TestRotationRetainsCheckpointWhenLiveReadsCurrentFirst(t *testing.T) {
	ctx := context.Background()
	database, store, current := checkpointDatabase(t)
	parser := NewParser(store)
	lines := []string{`{"PersonaId":"self"}`, checkpointCompletedMatch("rank-a", "self"), `<== RankGetCombinedRankInfo(old-rank)`, `{"constructedLevel":3}`, checkpointCompletedMatch("rank-b", "self")}
	if err := writeLogLines(current, lines, false); err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(filepath.Dir(current), "Player-prev.log")
	if err := os.Rename(current, previous); err != nil {
		t.Fatal(err)
	}
	if err := writeLogLines(current, []string{`{"PersonaId":"self"}`}, false); err != nil {
		t.Fatal(err)
	}
	// The running live loop polls only Player.log after its initial tick.
	if _, err := parser.ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	if stats, err := NewParser(store).ParseFile(ctx, previous, true); err != nil {
		t.Fatal(err)
	} else if stats.LinesRead != 0 {
		t.Fatalf("lost rotated checkpoint after reading current log: replayed %d lines", stats.LinesRead)
	}
	if err := writeLogLines(current, []string{`<== RankGetCombinedRankInfo(new-rank)`, `{"constructedLevel":9}`}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
		t.Fatal(err)
	}
	var level int
	if err := database.QueryRow(`SELECT constructed_level FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='rank-b'`).Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != 9 {
		t.Fatalf("B rank=%d, want 9", level)
	}
}

func TestAccountChangeReplacesCheckpointIdentity(t *testing.T) {
	for _, identity := range []string{`{"PersonaId":"second-account"}`, `{"clientId":"second-account"}`, `Match to second-account:`, `{"payload":"{\"PersonaId\":\"second-account\"}"}`} {
		for _, appendLog := range []bool{false, true} {
			for _, restart := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/append_%v/restart_%v", identity, appendLog, restart), func(t *testing.T) {
					ctx := context.Background()
					database, store, current := checkpointDatabase(t)
					parser := NewParser(store)
					if err := writeLogLines(current, []string{`{"PersonaId":"first-account"}`, checkpointCompletedMatch("first-match", "first-account")}, false); err != nil {
						t.Fatal(err)
					}
					if _, err := parser.ParseFile(ctx, current, true); err != nil {
						t.Fatal(err)
					}
					if restart {
						parser = NewParser(store)
					}
					lines := []string{identity, checkpointFixture()[3], `<== RankGetCombinedRankInfo(login-rank)`, `{"constructedLevel":7}`, checkpointCompletedMatch("second-match", "second-account"), `<== RankGetCombinedRankInfo(new-rank)`, `{"constructedLevel":9}`}
					if err := writeLogLines(current, lines, appendLog); err != nil {
						t.Fatal(err)
					}
					if _, err := parser.ParseFile(ctx, current, true); err != nil {
						t.Fatal(err)
					}
					var result string
					var seat int
					if err := database.QueryRow(`SELECT COALESCE(result,''), COALESCE(player_seat_id,0) FROM matches WHERE arena_match_id='second-match'`).Scan(&result, &seat); err != nil {
						t.Fatal(err)
					}
					if result != "loss" || seat != 2 {
						t.Fatalf("new account result=%q seat=%d, want loss/2", result, seat)
					}
					var rank, oldRanks, oldPlays int
					if err := database.QueryRow(`SELECT constructed_level FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='second-match'`).Scan(&rank); err != nil {
						t.Fatal(err)
					}
					if err := database.QueryRow(`SELECT count(*) FROM match_rank_snapshots r JOIN matches m ON m.id=r.match_id WHERE m.arena_match_id='first-match'`).Scan(&oldRanks); err != nil {
						t.Fatal(err)
					}
					if err := database.QueryRow(`SELECT count(*) FROM match_card_plays c JOIN matches m ON m.id=c.match_id WHERE m.arena_match_id='first-match'`).Scan(&oldPlays); err != nil {
						t.Fatal(err)
					}
					if rank != 9 || oldRanks != 0 || oldPlays != 0 {
						t.Fatalf("account context leaked: new rank=%d, old ranks=%d, old plays=%d", rank, oldRanks, oldPlays)
					}
				})
			}
		}
	}
}

func TestRepeatedOrPlaceholderIdentityPreservesMatchContext(t *testing.T) {
	for _, identity := range []string{`{"PersonaId":"self-user"}`, `{"PersonaId":"NoInstallID-placeholder"}`, `{"clientId":"NoInstallID-placeholder"}`} {
		t.Run(identity, func(t *testing.T) {
			ctx := context.Background()
			database, store, current := checkpointDatabase(t)
			lines := checkpointFixture()
			if err := writeLogLines(current, lines[:3], false); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
				t.Fatal(err)
			}
			if err := writeLogLines(current, append([]string{identity}, lines[3:]...), true); err != nil {
				t.Fatal(err)
			}
			if _, err := NewParser(store).ParseFile(ctx, current, true); err != nil {
				t.Fatal(err)
			}
			got := checkpointRows(t, database)
			if len(got["match_card_plays"]) != 3 || len(got["match_replay_frames"]) != 3 {
				t.Fatalf("identity refresh dropped active match context: %#v", got)
			}
		})
	}
}
