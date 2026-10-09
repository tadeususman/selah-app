package models

import (
	"regexp"
	"time"
)

type User struct {
	ID           int64
	Email        string
	PasswordHash string
	Name         string
	CreatedAt    time.Time
}

type JournalEntry struct {
	ID             int64
	UserID         int64
	DayNumber      int
	EntryDate      time.Time
	EntryTime      time.Time
	Location       string
	VerseRef       string
	VerseText      string
	VerseParts     string // raw JSON [{"r":"Yohanes 3:16","t":"..."}]; "" if not a fetched passage
	CardVerseRef   string // verse picked for the Momen card; "" = default
	CardVerseText  string
	AIBackground   string
	Reflection     string
	PracticalStep  string
	Status         string // "draft" | "completed"
	ShareSummary   string
	PlanID         int64 // 0 if entry is not part of a plan
	PlanDay        int   // 0 if entry is not part of a plan
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Preview is a trimmed-down shape for the dashboard list, matching
// the "Day 243 / It was but a nice day... / 14th July, 2023" cards
// in the reference UI.
type Preview struct {
	ID           int64
	DayNumber    int
	Snippet      string
	EntryDate    time.Time
	EntryTime    time.Time
	Location     string
	VerseRef     string
	VerseText    string
	Status       string
	PlanID       int64  // 0 if not part of a plan
	PlanDay      int    // 0 if not part of a plan
	PlanName     string // "" if not part of a plan
	PlanDuration int    // 0 if not part of a plan
}

type JournalMessage struct {
	ID        int64
	EntryID   int64
	Role      string // "user" | "ai"
	Content   string
	CreatedAt time.Time
}

type Plan struct {
	ID              int64
	UserID          int64
	Name            string
	CoverText       string
	Duration        int
	Status          string // "active" | "completed"
	FinalReflection string
	ShareSummary    string
	Location        string
	CreatedAt       time.Time
	CompletedAt     *time.Time
}

type PlanDay struct {
	ID        int64
	PlanID    int64
	DayNumber int
	VerseRef  string
	VerseText string
	IntroText string
}

// PlanPreview is a trimmed shape for the /plan list page: plan + progress counter.
type PlanPreview struct {
	ID          int64
	Name        string
	CoverText   string
	Duration    int
	Status      string
	CompletedAt time.Time // zero value when still active
	CreatedAt   time.Time
	DoneCount   int // # of plan days with a completed journal entry
}

var passageRefRe = regexp.MustCompile(`\d\s*[-–—,]\s*\d`)

// IsPassageRef reports whether a verse reference spans several verses
// (e.g. "Yohanes 3:16-21", "Roma 8:28,31"), i.e. a perikop.
func IsPassageRef(ref string) bool { return passageRefRe.MatchString(ref) }
