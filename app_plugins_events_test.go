package main

import (
	"encoding/json"
	"testing"
	"time"
)

// The two EDGE events are what let the voice plugin replace the host's own
// speech. They are asserted here rather than through the pomodoro scheduler
// because the contract under test is the emit itself: exactly one frame, the
// right name, and a payload of FACTS with no wording in it.

// TestPhaseEndedCarriesTheHostsConclusion covers timer.phaseEnded.
//
// The payload is "focus" or "break" rather than the host's own five phase names,
// because that reduction is the conclusion a plugin would otherwise have to
// re-derive. It is asserted as an exact string, so a future edit that starts
// forwarding raw phase names fails here rather than in a plugin.
func TestPhaseEndedCarriesTheHostsConclusion(t *testing.T) {
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})
	if _, err := a.PluginRequest(id, "events.subscribe", `{"names":["timer.phaseEnded"]}`); err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}

	// The caller passes the conclusion already reduced ("focus" / "break").
	// phaseKind and store.IsFocusPhase were the pomodoro state machine's, and the
	// host has no phases to classify any more.
	a.notifyPluginPhaseEnded("focus")
	events := collected("timer.phaseEnded")
	if len(events) != 1 {
		t.Fatalf("want exactly 1 timer.phaseEnded, got %d (%+v)", len(events), events)
	}
	if events[0].PluginID != id {
		t.Fatalf("the event names %q, want %q", events[0].PluginID, id)
	}
	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("the payload is %T, want a plain object", events[0].Payload)
	}
	if payload["phase"] != "focus" {
		t.Fatalf("phase = %v, want focus", payload["phase"])
	}
	// No wording may travel with it: choosing what to say is the plugin's job,
	// and a "text" field here would quietly move it back into the host.
	if len(payload) != 1 {
		t.Fatalf("the payload carries %d fields (%v); it must carry only the fact", len(payload), payload)
	}

	// There is no break classification to assert any more: it lived in phaseKind,
	// which went with the timer.
}

// TestReminderFiredCarriesTheCounts covers reminder.fired.
//
// The three counts travel even though the host's own fixed clips could not use
// them - that is exactly the capability a plugin brings, so the payload must not
// be reduced to a single "something is due" flag.
func TestReminderFiredCarriesTheCounts(t *testing.T) {
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})
	if _, err := a.PluginRequest(id, "events.subscribe", `{"names":["reminder.fired"]}`); err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}

	a.notifyPluginReminderFired(3, 2, 9)
	events := collected("reminder.fired")
	if len(events) != 1 {
		t.Fatalf("want exactly 1 reminder.fired, got %d (%+v)", len(events), events)
	}
	if events[0].PluginID != id {
		t.Fatalf("the event names %q, want %q", events[0].PluginID, id)
	}
	// Round-trip the payload through JSON so the assertion is about what the
	// frontend will actually receive, not about Go's map typing.
	raw, err := json.Marshal(events[0].Payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		Due     int `json:"due"`
		Overdue int `json:"overdue"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Due != 3 || got.Overdue != 2 || got.Total != 9 {
		t.Fatalf("the counts are %+v, want due=3 overdue=2 total=9", got)
	}
}

// TestEdgeEventsOnlyReachSubscribers pins that these are subscriptions like any
// other: a plugin that did not ask receives nothing, so a plugin cannot listen in
// on the pet's timer by accident.
func TestEdgeEventsOnlyReachSubscribers(t *testing.T) {
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	// Installed and granted events, but never subscribed.
	installSampleWith(t, a, map[string]any{
		"id":          "com.example.silent",
		"permissions": []string{"events"},
	})

	a.notifyPluginPhaseEnded("focus")
	a.notifyPluginReminderFired(1, 0, 1)

	if got := collected("timer.phaseEnded"); len(got) != 0 {
		t.Fatalf("an unsubscribed plugin received %d phase events", len(got))
	}
	if got := collected("reminder.fired"); len(got) != 0 {
		t.Fatalf("an unsubscribed plugin received %d reminder events", len(got))
	}
}

// TestEdgeEventsAreSubscribable pins that the two names are in the closed set, so
// a plugin asking for them is not refused as unknown.
func TestEdgeEventsAreSubscribable(t *testing.T) {
	a := pluginApp(t)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})
	out, err := a.PluginRequest(id, "events.subscribe",
		`{"names":["timer.phaseEnded","reminder.fired"]}`)
	if err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}
	var names []string
	if err := json.Unmarshal([]byte(out), &names); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(names) != 2 {
		t.Fatalf("subscribed to %v, want both edge events", names)
	}
}

// TestEveryEventCarriesTheHostsLocalTime covers the envelope's `at`.
//
// The field exists so a MODEL can be told when something happened. Delivery order
// alone cannot answer that: a frame that arrives after a stall still describes the
// moment the phase ended, and a consumer stamping it with its own clock turns
// "the focus ended at 14:32" into "I noticed at 14:37".
//
// The assertions are about SHAPE and ZONE, not about a particular instant: a test
// that compared a formatted string would break the day the machine's timezone
// changes, and would prove nothing about whether the value is local.
func TestEveryEventCarriesTheHostsLocalTime(t *testing.T) {
	a := pluginApp(t)
	collected := collectPluginEvents(a)
	id := installSampleWith(t, a, map[string]any{"permissions": []string{"events"}})
	if _, err := a.PluginRequest(id, "events.subscribe", `{"names":["timer.phaseEnded"]}`); err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}

	before := time.Now()
	a.notifyPluginPhaseEnded("focus")
	events := collected("timer.phaseEnded")
	if len(events) != 1 {
		t.Fatalf("want exactly 1 event, got %d", len(events))
	}

	raw := events[0].At
	if raw == "" {
		t.Fatal("the envelope carries no `at`: a consumer cannot tell when the event happened")
	}
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t.Fatalf("at = %q is not RFC 3339: %v", raw, err)
	}
	// LOCAL offset, not UTC: a plugin that prints this string to the user must be
	// showing them their own clock.
	_, wantOffset := before.Zone()
	_, gotOffset := at.Zone()
	if gotOffset != wantOffset {
		t.Errorf("at carries offset %ds (%q), want the local %ds", gotOffset, raw, wantOffset)
	}
	if d := at.Sub(before); d < -2*time.Second || d > 2*time.Second {
		t.Errorf("at = %v is %v away from now, want it stamped at emit time", at, d)
	}

	// Every event, not just the edge ones: the stamp lives in emitPluginEvent, and
	// this is the assertion that keeps it there rather than in one caller.
	a.notifyPluginReminderFired(1, 0, 1)
	if _, err := a.PluginRequest(id, "events.subscribe", `{"names":["reminder.fired"]}`); err != nil {
		t.Fatalf("events.subscribe: %v", err)
	}
	a.notifyPluginReminderFired(1, 0, 1)
	fired := collected("reminder.fired")
	if len(fired) == 0 {
		t.Fatal("no reminder.fired event was collected")
	}
	if _, err := time.Parse(time.RFC3339, fired[len(fired)-1].At); err != nil {
		t.Errorf("reminder.fired carries at = %q, which is not RFC 3339: %v",
			fired[len(fired)-1].At, err)
	}
}
