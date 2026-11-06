package cloud

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Event types on a project's stream.
const (
	EventRevision       = "revision"
	EventAccessRevoked  = "access_revoked"
	EventProjectDeleted = "project_deleted"
)

// Event is one server-sent event.
type Event struct {
	ID   string
	Type string
	Data string
}

// Revision is the data of a "revision" event.
type Revision struct {
	Rev    int    `json:"rev"`
	Source string `json:"source"`
	Actor  string `json:"actor"`
	At     string `json:"at"`
}

// Revision decodes a revision event's data.
func (e Event) Revision() (Revision, error) {
	var r Revision
	err := json.Unmarshal([]byte(e.Data), &r)
	return r, err
}

// IdleTimeout: the server pings every 25 s, so this long without a byte
// means the connection is dead.
const IdleTimeout = 70 * time.Second

// Events opens a project's event stream and calls fn for every event until
// the stream ends, ctx is done, or fn returns false. A clean end of stream
// (the server rotates it about hourly) returns nil; the caller reconnects.
func (c *Client) Events(parent context.Context, projectID string, lastEventID int, fn func(Event) bool) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.API+"/projects/"+pathID(projectID)+"/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if lastEventID > 0 {
		req.Header.Set("Last-Event-ID", strconv.Itoa(lastEventID))
	}
	// No overall timeout: the stream is long-lived. A watchdog cancels it
	// when it goes silent instead.
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if parent.Err() != nil {
			return parent.Err()
		}
		return &UnavailableError{Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return apiError(resp.StatusCode, data)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		return &UnavailableError{Err: fmt.Errorf("event stream answered with %q", ct)}
	}

	idle := time.AfterFunc(IdleTimeout, cancel)
	defer idle.Stop()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	var ev Event
	var data []string
	for sc.Scan() {
		idle.Reset(IdleTimeout)
		line := sc.Text()
		if line == "" {
			if ev.Type != "" || len(data) > 0 {
				if ev.Type == "" {
					ev.Type = "message"
				}
				ev.Data = strings.Join(data, "\n")
				if !fn(ev) {
					return nil
				}
			}
			ev, data = Event{ID: ev.ID}, nil
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // comment: ": ping", ": connected"
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.ID = value
		case "event":
			ev.Type = value
		case "data":
			data = append(data, value)
		}
	}
	if parent.Err() != nil {
		return parent.Err()
	}
	if ctx.Err() != nil {
		return &UnavailableError{Err: fmt.Errorf("event stream silent for %s", IdleTimeout)}
	}
	if err := sc.Err(); err != nil {
		return &UnavailableError{Err: err}
	}
	return nil
}
