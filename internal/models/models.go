package models

import "time"

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
	AIBackground   string
	Reflection     string
	PracticalStep  string
	Status         string // "draft" | "completed"
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Preview is a trimmed-down shape for the dashboard list, matching
// the "Day 243 / It was but a nice day... / 14th July, 2023" cards
// in the reference UI.
type Preview struct {
	ID        int64
	DayNumber int
	Snippet   string
	EntryDate time.Time
}

type JournalMessage struct {
	ID        int64
	EntryID   int64
	Role      string // "user" | "ai"
	Content   string
	CreatedAt time.Time
}
