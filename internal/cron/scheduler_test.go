package cron

import (
	"testing"
	"time"
)

func TestParseAndDue(t *testing.T) {
	s := NewScheduler()
	j, e := s.Add("5 14 2 9 3", "echo hi")
	if e != nil || j.ID == "" {
		t.Fatal(e)
	}
	due := s.Due(time.Date(2026, 9, 2, 14, 5, 0, 0, time.UTC))
	if len(due) != 1 {
		t.Fatalf("due=%v", due)
	}
	if _, e := Parse("* * *"); e == nil {
		t.Fatal("accepted three fields")
	}
}
