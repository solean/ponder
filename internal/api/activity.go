package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/solean/ponder/internal/model"
)

func validateActivityDays(days []model.ActivityDayBoundary) error {
	if len(days) < 1 || len(days) > 366 {
		return fmt.Errorf("activity requires between 1 and 366 days")
	}
	var previousDate, previousEnd time.Time
	for i, day := range days {
		date, dateErr := time.Parse("2006-01-02", day.Date)
		start, startErr := time.Parse(time.RFC3339Nano, day.Start)
		end, endErr := time.Parse(time.RFC3339Nano, day.End)
		if dateErr != nil || startErr != nil || endErr != nil {
			return fmt.Errorf("activity day %d has an invalid date or timestamp", i)
		}
		// Local days can be shorter or longer than 24 hours. The upper bound
		// permits DST and timezone transitions while bounding request work.
		if !end.After(start) || end.Sub(start) > 48*time.Hour {
			return fmt.Errorf("activity day %d has an invalid interval", i)
		}
		if i > 0 && (!date.Equal(previousDate.AddDate(0, 0, 1)) || !start.Equal(previousEnd)) {
			return fmt.Errorf("activity days must be consecutive with contiguous intervals")
		}
		previousDate, previousEnd = date, end
	}
	return nil
}

func (s *Server) handleActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := requireJSONContentType(r); err != nil {
		writeJSONBodyError(w, err)
		return
	}
	var request struct {
		Days []model.ActivityDayBoundary `json:"days"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid activity request")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid activity request")
		return
	}
	if err := validateActivityDays(request.Days); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	activity, err := s.store.DailyActivity(r.Context(), request.Days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, activity)
}
