package backend

// Local event CRUD — Phase 3.
//
// ARCHITECTURAL DECISION (please read before changing this file):
//
// Local events serialize to ICS blobs at write time so all existing read
// code — rrule_expand.go, alarm.go, the calendar views, EventDetail, the
// agenda grouping, and Calendar_ListEventsInRange — works identically on
// CalDAV-fetched and locally-composed events. The only new code path is
// the write direction.
//
// Alternatives considered and rejected:
//
//   - Store local events as columns only, fork the read path → would
//     duplicate the recurrence engine, the VALARM extractor, and the
//     view-side rendering. Hundreds of LOC of negative gain.
//   - Store as JSON, parse on display → loses the index benefit of the
//     denormalized columns AND duplicates the read path the same way.
//
// The ICS blob is the source of truth. Denormalized columns (summary,
// dtstart_unix, etc.) exist for fast queries but are always rebuilt from
// the blob on write. Phase 2 CalDAV write support will reuse the same
// serializeVEVENT helper — the only addition there is a PUT to the
// server, which is transport, not encoding.

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"
	"github.com/google/uuid"
)

// EventInput is the shape the frontend sends for create and update operations.
type EventInput struct {
	CalendarID  string          `json:"calendarId"`
	Summary     string          `json:"summary"`
	Description string          `json:"description,omitempty"`
	Location    string          `json:"location,omitempty"`
	DTStartUnix int64           `json:"dtstartUnix"`
	DTEndUnix   int64           `json:"dtendUnix"`
	IsAllDay    bool            `json:"isAllDay,omitempty"`
	Recurrence  *RecurrenceSpec `json:"recurrence,omitempty"`
	Reminder    *ReminderSpec   `json:"reminder,omitempty"`
}

// RecurrenceSpec describes the RRULE shape the composer offers in v1.
type RecurrenceSpec struct {
	Freq      string `json:"freq"`      // "DAILY" | "WEEKLY" | "MONTHLY" | "YEARLY"
	UntilUnix int64  `json:"untilUnix"` // 0 = open-ended (mutually exclusive with Count)
	Count     int    `json:"count"`     // 0 = open-ended
}

// ReminderSpec describes a single DISPLAY VALARM relative to DTSTART.
type ReminderSpec struct {
	OffsetMinutes int `json:"offsetMinutes"` // minutes BEFORE DTSTART
}

// EventCreateInput aliases EventInput for type clarity in Wails bindings.
type EventCreateInput = EventInput

// EventUpdateInput is EventInput + target event ID.
type EventUpdateInput struct {
	EventID string `json:"eventId"`
	EventInput
}

// EditScope controls how recurring-event updates and deletes behave.
type EditScope string

const (
	EditScopeThis          EditScope = "this"
	EditScopeThisAndFuture EditScope = "this-and-future"
	EditScopeAll           EditScope = "all"
)

// CreateEvent serializes the input as a VEVENT, inserts the events row
// plus any VALARM-driven event_alarms rows, and returns the new event ID.
// The bridge calls AlarmScheduler.Reevaluate() after.
func (a *API) CreateEvent(in EventInput) (string, error) {
	if err := validateInput(in); err != nil {
		return "", err
	}
	_, src, err := a.lookupCalendarAndSource(in.CalendarID)
	if err != nil {
		return "", err
	}
	if src.Type != SourceTypeLocal {
		return "", fmt.Errorf("calendar: event CRUD is only supported on local calendars (got type=%q)", src.Type)
	}

	uid := uuid.NewString() + "@aerion-local"
	icsBlob, err := serializeVEVENT(uid, in)
	if err != nil {
		return "", fmt.Errorf("serialize event: %w", err)
	}

	ev := Event{
		ID:          uuid.NewString(),
		CalendarID:  in.CalendarID,
		UID:         uid,
		Summary:     in.Summary,
		Description: in.Description,
		Location:    in.Location,
		DTStartUnix: in.DTStartUnix,
		DTEndUnix:   in.DTEndUnix,
		IsAllDay:    in.IsAllDay,
		RRuleText:   rruleText(in.Recurrence),
		ICSBlob:     icsBlob,
	}

	err = a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.UpsertEventTx(tx, ev); err != nil {
			return err
		}
		return a.extractAndUpsertAlarmsTx(tx, ev)
	})
	if err != nil {
		return "", fmt.Errorf("persist event: %w", err)
	}
	return ev.ID, nil
}

// UpdateEvent dispatches on scope. Non-recurring events ignore scope.
func (a *API) UpdateEvent(in EventUpdateInput, scope EditScope) error {
	if in.EventID == "" {
		return errors.New("calendar: event ID required")
	}
	if err := validateInput(in.EventInput); err != nil {
		return err
	}
	master, err := a.store.GetEvent(in.EventID)
	if err != nil {
		return fmt.Errorf("get event: %w", err)
	}
	if master == nil {
		return errors.New("calendar: event not found")
	}
	_, src, err := a.lookupCalendarAndSource(master.CalendarID)
	if err != nil {
		return err
	}
	if src.Type != SourceTypeLocal {
		return fmt.Errorf("calendar: event CRUD is only supported on local calendars")
	}

	// Non-recurring or scope=All → straight replace.
	if master.RRuleText == "" || scope == EditScopeAll || scope == "" {
		return a.updateAll(*master, in.EventInput)
	}

	switch scope {
	case EditScopeThis:
		return a.updateThis(*master, in.EventInput)
	case EditScopeThisAndFuture:
		return a.updateThisAndFuture(*master, in.EventInput)
	}
	return fmt.Errorf("calendar: unknown edit scope %q", scope)
}

// DeleteEvent removes an event with scope semantics symmetric to UpdateEvent.
func (a *API) DeleteEvent(eventID string, scope EditScope) error {
	if eventID == "" {
		return errors.New("calendar: event ID required")
	}
	master, err := a.store.GetEvent(eventID)
	if err != nil {
		return fmt.Errorf("get event: %w", err)
	}
	if master == nil {
		return nil // idempotent
	}
	_, src, err := a.lookupCalendarAndSource(master.CalendarID)
	if err != nil {
		return err
	}
	if src.Type != SourceTypeLocal {
		return fmt.Errorf("calendar: event CRUD is only supported on local calendars")
	}

	if master.RRuleText == "" || scope == EditScopeAll || scope == "" {
		// CASCADE removes overrides + alarms.
		return a.store.WithTx(func(tx *sql.Tx) error {
			return a.store.DeleteEventByUIDTx(tx, master.CalendarID, master.UID)
		})
	}

	// For recurring "this" / "this-and-future", the caller's intent is
	// based on a specific instance. The frontend passes the original
	// instance start via DTStartUnix on the input (we read master.DTStart
	// here as the placeholder for now — the bridge layer can override
	// when a specific instance UID is wired through in a follow-up).
	splitUnix := master.DTStartUnix

	switch scope {
	case EditScopeThis:
		return a.deleteThis(*master, splitUnix)
	case EditScopeThisAndFuture:
		return a.deleteThisAndFuture(*master, splitUnix)
	}
	return fmt.Errorf("calendar: unknown edit scope %q", scope)
}

// --- Internal helpers ---------------------------------------------------------

func validateInput(in EventInput) error {
	if in.CalendarID == "" {
		return errors.New("calendar: calendar ID required")
	}
	if in.Summary == "" {
		return errors.New("calendar: summary required")
	}
	if in.DTStartUnix == 0 {
		return errors.New("calendar: dtstart required")
	}
	if in.DTEndUnix == 0 {
		return errors.New("calendar: dtend required")
	}
	if in.DTEndUnix < in.DTStartUnix {
		return errors.New("calendar: dtend must be >= dtstart")
	}
	if in.Recurrence == nil {
		return nil
	}
	switch in.Recurrence.Freq {
	case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
	default:
		return fmt.Errorf("calendar: invalid recurrence freq %q", in.Recurrence.Freq)
	}
	if in.Recurrence.UntilUnix != 0 && in.Recurrence.Count != 0 {
		return errors.New("calendar: recurrence UntilUnix and Count are mutually exclusive")
	}
	return nil
}

func (a *API) lookupCalendarAndSource(calendarID string) (*Calendar, *Source, error) {
	sources, err := a.store.ListSources()
	if err != nil {
		return nil, nil, err
	}
	for i := range sources {
		cals, err := a.store.ListCalendars(sources[i].ID)
		if err != nil {
			continue
		}
		for j := range cals {
			if cals[j].ID == calendarID {
				return &cals[j], &sources[i], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("calendar: calendar %q not found", calendarID)
}

func rruleText(spec *RecurrenceSpec) string {
	if spec == nil {
		return ""
	}
	parts := []string{"FREQ=" + spec.Freq}
	if spec.UntilUnix > 0 {
		parts = append(parts, "UNTIL="+formatICSDateTime(time.Unix(spec.UntilUnix, 0)))
	}
	if spec.Count > 0 {
		parts = append(parts, fmt.Sprintf("COUNT=%d", spec.Count))
	}
	return strings.Join(parts, ";")
}

// setEventStartEnd writes DTSTART + DTEND on the event, choosing between
// DATE form (all-day) and DATE-TIME form (timed).
func setEventStartEnd(event *ical.Event, in EventInput) {
	if in.IsAllDay {
		setDateValue(event, ical.PropDateTimeStart, in.DTStartUnix)
		setDateValue(event, ical.PropDateTimeEnd, in.DTEndUnix)
		return
	}
	event.Props.SetDateTime(ical.PropDateTimeStart, time.Unix(in.DTStartUnix, 0).UTC())
	event.Props.SetDateTime(ical.PropDateTimeEnd, time.Unix(in.DTEndUnix, 0).UTC())
}

// serializeVEVENT builds a single-event VCALENDAR for events.ics_blob.
func serializeVEVENT(uid string, in EventInput) (string, error) {
	event := ical.NewEvent()
	event.Props.SetText(ical.PropUID, uid)
	event.Props.SetDateTime(ical.PropDateTimeStamp, time.Now().UTC())
	event.Props.SetText(ical.PropSummary, in.Summary)
	if in.Description != "" {
		event.Props.SetText(ical.PropDescription, in.Description)
	}
	if in.Location != "" {
		event.Props.SetText(ical.PropLocation, in.Location)
	}

	setEventStartEnd(event, in)

	if rt := rruleText(in.Recurrence); rt != "" {
		setRRuleText(event.Props, rt)
	}

	if in.Reminder != nil {
		alarm := &ical.Component{Name: ical.CompAlarm, Props: ical.Props{}}
		alarm.Props.SetText(ical.PropAction, "DISPLAY")
		trigger := ical.NewProp(ical.PropTrigger)
		trigger.Value = fmt.Sprintf("-PT%dM", in.Reminder.OffsetMinutes)
		alarm.Props.Add(trigger)
		alarm.Props.SetText(ical.PropDescription, in.Summary)
		event.Component.Children = append(event.Component.Children, alarm)
	}

	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, "-//Aerion//Calendar Extension//EN")
	cal.Children = append(cal.Children, event.Component)

	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// setDateValue stamps a DATE-only property for all-day events.
func setDateValue(event *ical.Event, propName string, unix int64) {
	prop := ical.NewProp(propName)
	prop.Value = time.Unix(unix, 0).UTC().Format("20060102")
	prop.Params = ical.Params{}
	prop.Params.Set(ical.ParamValue, "DATE")
	event.Props.Set(prop)
}

func formatICSDateTime(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}

// setRRuleText writes the RRULE value WITHOUT the TEXT-type semicolon escape
// go-ical applies by default via SetText. The RRULE property uses RECUR type
// per RFC 5545 §3.3.10 — semicolons are part-separators, not escaped.
func setRRuleText(props ical.Props, rt string) {
	prop := ical.NewProp(ical.PropRecurrenceRule)
	prop.SetValueType(ical.ValueRecurrence)
	prop.Value = rt
	props.Set(prop)
}

// extractAndUpsertAlarmsTx re-parses the event's blob, extracts VALARMs,
// and upserts them into event_alarms. Keeps create/update → alarms atomic.
func (a *API) extractAndUpsertAlarmsTx(tx *sql.Tx, ev Event) error {
	overrides, err := a.store.ListOverrides(ev.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	instances, err := ExpandInRange(ev, overrides, now, now.Add(7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("expand for alarms: %w", err)
	}
	alarms, err := ExtractAlarms(ev, overrides, instances)
	if err != nil {
		return fmt.Errorf("extract alarms: %w", err)
	}
	for _, alm := range alarms {
		if err := a.store.UpsertAlarmTx(tx, alm); err != nil {
			return err
		}
	}
	return nil
}

// --- Scope-aware update branches ---------------------------------------------

func (a *API) updateAll(master Event, in EventInput) error {
	icsBlob, err := serializeVEVENT(master.UID, in)
	if err != nil {
		return fmt.Errorf("serialize event: %w", err)
	}
	ev := master
	ev.Summary = in.Summary
	ev.Description = in.Description
	ev.Location = in.Location
	ev.DTStartUnix = in.DTStartUnix
	ev.DTEndUnix = in.DTEndUnix
	ev.IsAllDay = in.IsAllDay
	ev.RRuleText = rruleText(in.Recurrence)
	ev.ICSBlob = icsBlob

	return a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.UpsertEventTx(tx, ev); err != nil {
			return err
		}
		// Drop ALL overrides — they were attached to the old RRULE shape
		// and may not map cleanly to the new occurrence set. Users can
		// re-add per-instance overrides if needed.
		if _, err := tx.Exec(
			`DELETE FROM event_recurrence_overrides WHERE event_id = ?`, ev.ID); err != nil {
			return err
		}
		return a.extractAndUpsertAlarmsTx(tx, ev)
	})
}

func (a *API) updateThis(master Event, in EventInput) error {
	icsBlob, err := serializeVEVENTWithRecurrenceID(master.UID, in)
	if err != nil {
		return fmt.Errorf("serialize override: %w", err)
	}
	return a.store.WithTx(func(tx *sql.Tx) error {
		return a.store.UpsertOverrideTx(tx, master.ID, in.DTStartUnix, icsBlob)
	})
}

func (a *API) updateThisAndFuture(master Event, in EventInput) error {
	splitUnix := in.DTStartUnix

	// 1. Clamp master's RRULE with UNTIL = splitUnix - 1s.
	clampedRRULE := clampRRuleUntil(master.RRuleText, splitUnix-1)
	clampedICS, err := reserializeMasterICS(master, clampedRRULE)
	if err != nil {
		return err
	}
	clampedMaster := master
	clampedMaster.RRuleText = clampedRRULE
	clampedMaster.ICSBlob = clampedICS

	// 2. Build the new master starting from splitUnix with input fields.
	newUID := uuid.NewString() + "@aerion-local"
	newICS, err := serializeVEVENT(newUID, in)
	if err != nil {
		return err
	}
	newMaster := Event{
		ID:          uuid.NewString(),
		CalendarID:  master.CalendarID,
		UID:         newUID,
		Summary:     in.Summary,
		Description: in.Description,
		Location:    in.Location,
		DTStartUnix: in.DTStartUnix,
		DTEndUnix:   in.DTEndUnix,
		IsAllDay:    in.IsAllDay,
		RRuleText:   rruleText(in.Recurrence),
		ICSBlob:     newICS,
	}

	return a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.UpsertEventTx(tx, clampedMaster); err != nil {
			return err
		}
		// Drop future overrides — they belong to a series with the new UID.
		if _, err := tx.Exec(
			`DELETE FROM event_recurrence_overrides WHERE event_id = ? AND recurrence_id_unix >= ?`,
			master.ID, splitUnix,
		); err != nil {
			return err
		}
		if err := a.store.UpsertEventTx(tx, newMaster); err != nil {
			return err
		}
		if err := a.extractAndUpsertAlarmsTx(tx, clampedMaster); err != nil {
			return err
		}
		return a.extractAndUpsertAlarmsTx(tx, newMaster)
	})
}

// --- Scope-aware delete branches ---------------------------------------------

func (a *API) deleteThis(master Event, instanceUnix int64) error {
	updatedICS, err := addEXDATE(master.ICSBlob, instanceUnix)
	if err != nil {
		return err
	}
	updated := master
	updated.ICSBlob = updatedICS
	return a.store.WithTx(func(tx *sql.Tx) error {
		if err := a.store.UpsertEventTx(tx, updated); err != nil {
			return err
		}
		return a.extractAndUpsertAlarmsTx(tx, updated)
	})
}

func (a *API) deleteThisAndFuture(master Event, instanceUnix int64) error {
	clampedRRULE := clampRRuleUntil(master.RRuleText, instanceUnix-1)
	clampedICS, err := reserializeMasterICS(master, clampedRRULE)
	if err != nil {
		return err
	}
	updated := master
	updated.RRuleText = clampedRRULE
	updated.ICSBlob = clampedICS
	return a.store.WithTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`DELETE FROM event_recurrence_overrides WHERE event_id = ? AND recurrence_id_unix >= ?`,
			master.ID, instanceUnix,
		); err != nil {
			return err
		}
		if err := a.store.UpsertEventTx(tx, updated); err != nil {
			return err
		}
		return a.extractAndUpsertAlarmsTx(tx, updated)
	})
}

// --- ICS manipulation helpers -------------------------------------------------

// serializeVEVENTWithRecurrenceID is like serializeVEVENT but uses the
// caller-supplied uid (same as the master's per RFC 5545 §3.8.4.4) and
// adds RECURRENCE-ID = DTStartUnix so the override binds to a specific
// occurrence.
func serializeVEVENTWithRecurrenceID(uid string, in EventInput) (string, error) {
	// Overrides are single-instance, never recurring.
	in.Recurrence = nil

	icsBlob, err := serializeVEVENT(uid, in)
	if err != nil {
		return "", err
	}
	cal, err := ical.NewDecoder(strings.NewReader(icsBlob)).Decode()
	if err != nil {
		return "", err
	}
	if len(cal.Events()) == 0 {
		return "", errors.New("calendar: re-encoded event has no VEVENT")
	}
	ev := cal.Events()[0]
	recIDProp := ical.NewProp(ical.PropRecurrenceID)
	recIDProp.Value = formatICSDateTime(time.Unix(in.DTStartUnix, 0))
	ev.Props.Set(recIDProp)
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// reserializeMasterICS rewrites the RRULE on an existing master's ICS blob.
func reserializeMasterICS(master Event, newRRULE string) (string, error) {
	cal, err := ical.NewDecoder(strings.NewReader(master.ICSBlob)).Decode()
	if err != nil {
		return "", err
	}
	if len(cal.Events()) == 0 {
		return "", errors.New("calendar: master ICS has no VEVENT")
	}
	ev := cal.Events()[0]
	if newRRULE == "" {
		ev.Props.Del(ical.PropRecurrenceRule)
	}
	if newRRULE != "" {
		setRRuleText(ev.Props, newRRULE)
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// addEXDATE injects an EXDATE property onto the master's VEVENT.
func addEXDATE(icsBlob string, instanceUnix int64) (string, error) {
	cal, err := ical.NewDecoder(strings.NewReader(icsBlob)).Decode()
	if err != nil {
		return "", err
	}
	if len(cal.Events()) == 0 {
		return "", errors.New("calendar: master ICS has no VEVENT")
	}
	ev := cal.Events()[0]
	exdateStr := formatICSDateTime(time.Unix(instanceUnix, 0))

	existing := ev.Props.Get(ical.PropExceptionDates)
	if existing != nil {
		existing.Value = existing.Value + "," + exdateStr
	}
	if existing == nil {
		p := ical.NewProp(ical.PropExceptionDates)
		p.Value = exdateStr
		ev.Props.Set(p)
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// clampRRuleUntil returns the RRULE text with an UNTIL=<unix> clause added,
// replacing any existing UNTIL or COUNT.
func clampRRuleUntil(rrule string, untilUnix int64) string {
	if rrule == "" {
		return ""
	}
	body := strings.TrimPrefix(rrule, "RRULE:")
	parts := strings.Split(body, ";")
	out := make([]string, 0, len(parts)+1)
	for _, p := range parts {
		upper := strings.ToUpper(strings.SplitN(p, "=", 2)[0])
		if upper == "UNTIL" || upper == "COUNT" {
			continue
		}
		out = append(out, p)
	}
	out = append(out, "UNTIL="+formatICSDateTime(time.Unix(untilUnix, 0)))
	return strings.Join(out, ";")
}
