package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/solean/ponder/internal/model"
)

func TestMatchTagsEndpoint(t *testing.T) {
	t.Parallel()

	store := seedMatchListStore(t, 3)
	handler := NewServer(store, "", nil).Handler()
	matches := getMatchList(t, store, "/api/matches").Matches
	first, second, untouched := matches[0].ID, matches[1].ID, matches[2].ID

	post := func(matchID int64, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		path := fmt.Sprintf("/api/matches/%d/tags", matchID)
		handler.ServeHTTP(rec, localRequest(http.MethodPost, path, strings.NewReader(body)))
		return rec
	}
	tagsByID := func() map[int64][]string {
		t.Helper()
		out := map[int64][]string{}
		for _, match := range getMatchList(t, store, "/api/matches").Matches {
			out[match.ID] = match.Tags
		}
		return out
	}
	usage := func() []model.MatchTagUsage {
		t.Helper()
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, localRequest(http.MethodGet, "/api/match-tags", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET match-tags: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var out []model.MatchTagUsage
		if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
			t.Fatalf("decode usage: %v", err)
		}
		return out
	}

	if got := tagsByID()[untouched]; got == nil || len(got) != 0 {
		t.Fatalf("untagged match tags = %#v, want empty non-nil list", got)
	}
	if got := usage(); len(got) != 0 {
		t.Fatalf("usage before tagging = %v, want none", got)
	}

	// Normalised: trimmed, single-spaced, lowercased, de-duplicated, sorted.
	rec := post(first, `{"tags":["  Land   Flooded ","misplayed","LAND FLOODED",""]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set tags: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"tags":["land flooded","misplayed"]`) {
		t.Fatalf("set tags response = %s", rec.Body.String())
	}
	if rec := post(second, `{"tags":["misplayed"]}`); rec.Code != http.StatusOK {
		t.Fatalf("set second tags: status=%d", rec.Code)
	}

	tags := tagsByID()
	if want := []string{"land flooded", "misplayed"}; !reflect.DeepEqual(tags[first], want) {
		t.Fatalf("first tags = %v, want %v", tags[first], want)
	}
	if want := []string{"misplayed"}; !reflect.DeepEqual(tags[second], want) {
		t.Fatalf("second tags = %v, want %v", tags[second], want)
	}
	wantUsage := []model.MatchTagUsage{{Tag: "misplayed", Count: 2}, {Tag: "land flooded", Count: 1}}
	if got := usage(); !reflect.DeepEqual(got, wantUsage) {
		t.Fatalf("usage = %v, want %v", got, wantUsage)
	}

	detail := httptest.NewRecorder()
	handler.ServeHTTP(detail, localRequest(http.MethodGet, fmt.Sprintf("/api/matches/%d", first), nil))
	if !strings.Contains(detail.Body.String(), `"tags":["land flooded","misplayed"]`) {
		t.Fatalf("detail did not carry tags: status=%d", detail.Code)
	}

	// Replacing the set removes tags left out; an empty list clears them.
	if rec := post(first, `{"tags":["mulligan"]}`); rec.Code != http.StatusOK {
		t.Fatalf("replace tags: status=%d", rec.Code)
	}
	if rec := post(second, `{"tags":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("clear tags: status=%d", rec.Code)
	}
	tags = tagsByID()
	if want := []string{"mulligan"}; !reflect.DeepEqual(tags[first], want) {
		t.Fatalf("first tags after replace = %v, want %v", tags[first], want)
	}
	if len(tags[second]) != 0 {
		t.Fatalf("second tags after clear = %v, want none", tags[second])
	}

	cases := []struct {
		name    string
		matchID int64
		body    string
		want    int
	}{
		{"missing match", untouched + 100, `{"tags":["x"]}`, http.StatusNotFound},
		{"missing field", first, `{}`, http.StatusBadRequest},
		{"tag too long", first, `{"tags":["` + strings.Repeat("a", 41) + `"]}`, http.StatusBadRequest},
		{"too many tags", first, `{"tags":[` + manyTags(21) + `]}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		if rec := post(tc.matchID, tc.body); rec.Code != tc.want {
			t.Fatalf("%s: status=%d, want %d; body=%s", tc.name, rec.Code, tc.want, rec.Body.String())
		}
	}
	if want := []string{"mulligan"}; !reflect.DeepEqual(tagsByID()[first], want) {
		t.Fatal("a rejected request changed the stored tags")
	}
}

func manyTags(count int) string {
	parts := make([]string, count)
	for index := range parts {
		parts[index] = fmt.Sprintf(`"tag %d"`, index)
	}
	return strings.Join(parts, ",")
}
