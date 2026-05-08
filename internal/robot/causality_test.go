package robot

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuildCausalityOutput_SortsDeterministically(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)
	t2 := t0.Add(2 * time.Minute)

	loaders := causalityLoaders{
		audit: func(opts CausalityOptions, since, until *time.Time) ([]CausalityEvent, error) {
			return []CausalityEvent{
				{ID: "z", Source: "robot_audit", Type: "command", ts: t1},
				{ID: "a", Source: "robot_audit", Type: "command", ts: t0},
			}, nil
		},
		mail: func(opts CausalityOptions) ([]CausalityEvent, []CausalitySourceStatus, []string) {
			return []CausalityEvent{
				{ID: "m1", Source: "agentmail_inbox", Type: "message", ts: t1},
			}, []CausalitySourceStatus{{Name: "agentmail_inbox", Available: true, Events: 1}}, nil
		},
		session: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return []CausalityEvent{{ID: "s", Source: "session_timeline", Type: "working", ts: t1}}, nil
		},
		pipeline: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return []CausalityEvent{{ID: "p", Source: "pipeline_state", Type: "pipeline_finished", ts: t2}}, nil
		},
	}

	out := buildCausalityOutput(CausalityOptions{Session: "myproj", Limit: 50}, loaders)
	if !out.Success {
		t.Fatalf("expected success=true, got error=%q", out.Error)
	}
	if len(out.Events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(out.Events))
	}

	for i := 1; i < len(out.Events); i++ {
		prev := out.Events[i-1]
		cur := out.Events[i]
		if cur.ts.Before(prev.ts) {
			t.Fatalf("events not sorted at index %d: %s before %s", i, cur.ts, prev.ts)
		}
		if cur.ts.Equal(prev.ts) {
			if cur.Source < prev.Source {
				t.Fatalf("source tie-break violated at index %d: %s < %s", i, cur.Source, prev.Source)
			}
			if cur.Source == prev.Source && cur.ID < prev.ID {
				t.Fatalf("id tie-break violated at index %d: %s < %s", i, cur.ID, prev.ID)
			}
		}
	}
}

func TestBuildCausalityOutput_FiltersByWindowAndFields(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(1 * time.Minute)
	t2 := t0.Add(2 * time.Minute)

	loaders := causalityLoaders{
		audit: func(opts CausalityOptions, since, until *time.Time) ([]CausalityEvent, error) {
			return []CausalityEvent{
				{ID: "1", Source: "robot_audit", Type: "command", Session: "myproj", Pane: "1", BeadID: "bd-1", ChainID: "c1", ts: t0},
				{ID: "2", Source: "robot_audit", Type: "command", Session: "myproj", Pane: "2", BeadID: "bd-2", ChainID: "c2", ts: t1},
				{ID: "3", Source: "robot_audit", Type: "send", Session: "myproj", Pane: "1", BeadID: "bd-1", ChainID: "c1", ts: t2},
			}, nil
		},
		mail: func(opts CausalityOptions) ([]CausalityEvent, []CausalitySourceStatus, []string) {
			return nil, []CausalitySourceStatus{{Name: "agentmail_inbox", Available: true, Events: 0}}, nil
		},
		session:  func(opts CausalityOptions) ([]CausalityEvent, error) { return nil, nil },
		pipeline: func(opts CausalityOptions) ([]CausalityEvent, error) { return nil, nil },
	}

	out := buildCausalityOutput(CausalityOptions{
		Session: "myproj",
		BeadID:  "bd-1",
		Pane:    "1",
		Type:    "command",
		Chain:   "c1",
		Since:   t0.Add(-30 * time.Second).Format(time.RFC3339),
		Until:   t1.Format(time.RFC3339),
		Limit:   10,
	}, loaders)
	if !out.Success {
		t.Fatalf("expected success=true, got error=%q", out.Error)
	}
	if out.Filtered != 1 {
		t.Fatalf("expected filtered=1, got %d", out.Filtered)
	}
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}
	if out.Events[0].ID != "1" {
		t.Fatalf("expected ID=1, got %q", out.Events[0].ID)
	}
}

func TestBuildCausalityOutput_SessionFilterKeepsSessionAgnosticMailEvents(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 12, 30, 0, 0, time.UTC)

	loaders := causalityLoaders{
		audit: func(opts CausalityOptions, since, until *time.Time) ([]CausalityEvent, error) {
			return nil, nil
		},
		mail: func(opts CausalityOptions) ([]CausalityEvent, []CausalitySourceStatus, []string) {
			return []CausalityEvent{
					{ID: "m1", Source: "agentmail_inbox", Type: "message", Session: "", ts: t0},
					{ID: "m2", Source: "agentmail_inbox", Type: "message", Session: "other-session", ts: t0.Add(1 * time.Second)},
				},
				[]CausalitySourceStatus{{Name: "agentmail_inbox", Available: true, Events: 2}},
				nil
		},
		session:  func(opts CausalityOptions) ([]CausalityEvent, error) { return nil, nil },
		pipeline: func(opts CausalityOptions) ([]CausalityEvent, error) { return nil, nil },
	}

	out := buildCausalityOutput(CausalityOptions{
		Session: "my-session",
		Limit:   10,
	}, loaders)
	if !out.Success {
		t.Fatalf("expected success=true, got error=%q", out.Error)
	}
	if out.Filtered != 1 {
		t.Fatalf("expected filtered=1, got %d", out.Filtered)
	}
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}
	if out.Events[0].ID != "m1" {
		t.Fatalf("expected session-agnostic event m1, got %q", out.Events[0].ID)
	}
	if out.Events[0].Session != "" {
		t.Fatalf("expected empty session on mail event, got %q", out.Events[0].Session)
	}
}

func TestBuildCausalityOutput_MissingSourceDegrades(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 14, 0, 0, 0, time.UTC)
	loaders := causalityLoaders{
		audit: func(opts CausalityOptions, since, until *time.Time) ([]CausalityEvent, error) {
			return nil, errors.New("audit unavailable")
		},
		mail: func(opts CausalityOptions) ([]CausalityEvent, []CausalitySourceStatus, []string) {
			return nil,
				[]CausalitySourceStatus{{Name: "agentmail_reservations", Available: false, Error: "mail down"}},
				[]string{"agentmail down"}
		},
		session: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return nil, errors.New("timeline missing")
		},
		pipeline: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return []CausalityEvent{{ID: "p1", Source: "pipeline_state", Type: "pipeline_started", ts: t0}}, nil
		},
	}

	out := buildCausalityOutput(CausalityOptions{Session: "myproj", Limit: 10}, loaders)
	if !out.Success {
		t.Fatalf("expected success=true, got error=%q", out.Error)
	}
	if len(out.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(out.Events))
	}
	if out.Events[0].Source != "pipeline_state" {
		t.Fatalf("expected pipeline_state event, got %q", out.Events[0].Source)
	}
	if len(out.Warnings) == 0 {
		t.Fatal("expected warnings when sources are unavailable")
	}

	foundUnavailable := false
	for _, src := range out.Sources {
		if !src.Available {
			foundUnavailable = true
			break
		}
	}
	if !foundUnavailable {
		t.Fatal("expected at least one unavailable source status")
	}
}

func TestBuildCausalityOutput_BeadChainMiniWorkflow(t *testing.T) {
	t0 := time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)
	t1 := t0.Add(20 * time.Second)
	t2 := t0.Add(40 * time.Second)
	beadID := "bd-2mb03.4"

	loaders := causalityLoaders{
		audit: func(opts CausalityOptions, since, until *time.Time) ([]CausalityEvent, error) {
			return []CausalityEvent{{ID: "a1", Source: "robot_audit", Type: "command", BeadID: beadID, Pane: "2", ChainID: "cmd-100", ts: t0}}, nil
		},
		mail: func(opts CausalityOptions) ([]CausalityEvent, []CausalitySourceStatus, []string) {
			events := []CausalityEvent{{ID: "m1", Source: "agentmail_reservations", Type: "reservation_active", BeadID: beadID, Agent: "BlueLake", ts: t1}}
			return events,
				[]CausalitySourceStatus{{Name: "agentmail_reservations", Available: true, Events: 1}},
				nil
		},
		session: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return nil, nil
		},
		pipeline: func(opts CausalityOptions) ([]CausalityEvent, error) {
			return []CausalityEvent{{ID: "p1", Source: "pipeline_state", Type: "pipeline_started", BeadID: beadID, ChainID: "run-xyz", ts: t2}}, nil
		},
	}

	out := buildCausalityOutput(CausalityOptions{Session: "myproj", BeadID: beadID, Limit: 10}, loaders)
	if !out.Success {
		t.Fatalf("expected success=true, got error=%q", out.Error)
	}
	if out.Filtered != 3 {
		t.Fatalf("expected filtered=3, got %d", out.Filtered)
	}
	if len(out.Events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(out.Events))
	}

	gotSources := []string{out.Events[0].Source, out.Events[1].Source, out.Events[2].Source}
	joined := strings.Join(gotSources, ",")
	if joined != "robot_audit,agentmail_reservations,pipeline_state" {
		t.Fatalf("unexpected source order: %s", joined)
	}
}
