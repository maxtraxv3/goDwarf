package main

import (
	"sync"
	"time"
)

type timedMessage struct {
	Text string
	Time time.Time
}

type messageLog struct {
	mu      sync.Mutex
	entries []timedMessage
	max     int
}

func (l *messageLog) Add(msg string) {
	if msg == "" {
		return
	}
	if wasmPrivacyActive() {
		return
	}
	entry := timedMessage{Text: msg, Time: time.Now()}

	l.mu.Lock()
	l.entries = append(l.entries, entry)
	if len(l.entries) > l.max {
		l.entries = l.entries[len(l.entries)-l.max:]
	}
	l.mu.Unlock()
}

func (l *messageLog) Entries(format string, useTimestamps bool) []string {
	l.mu.Lock()
	entries := make([]timedMessage, len(l.entries))
	copy(entries, l.entries)
	l.mu.Unlock()

	out := make([]string, len(entries))
	if format == "" {
		format = "3:04PM"
	}
	if useTimestamps {
		for i, msg := range entries {
			out[i] = "[" + msg.Time.Format(format) + "] " + msg.Text
		}
		return out
	}
	for i, msg := range entries {
		out[i] = msg.Text
	}
	return out
}
