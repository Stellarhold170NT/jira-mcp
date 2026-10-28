package jiramcp

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mmatczuk/jira-mcp/internal/jira"
)

// --- mock additions ---

func (m *mockClient) APIVersion() int {
	if m.apiVersion != 0 {
		return m.apiVersion
	}
	return 3
}

func (m *mockClient) AddWorklog(ctx context.Context, issueKey, started string, seconds int, comment string) (string, error) {
	if m.AddWorklogFn == nil {
		panic("mockClient.AddWorklog called but AddWorklogFn not set (issueKey=" + issueKey + ", started=" + started + ")")
	}
	return m.AddWorklogFn(ctx, issueKey, started, seconds, comment)
}

func (m *mockClient) GetWorklogs(ctx context.Context, issueKey string) ([]jira.WorklogEntry, error) {
	if m.GetWorklogsFn == nil {
		panic("mockClient.GetWorklogs called but GetWorklogsFn not set (issueKey=" + issueKey + ")")
	}
	return m.GetWorklogsFn(ctx, issueKey)
}

// --- pure helpers ---

func TestNormalizeTZOffset(t *testing.T) {
	ok := map[string]string{"+0700": "+0700", "+07:00": "+0700", "-0530": "-0530", "-05:30": "-0530", "+0000": "+0000"}
	for in, want := range ok {
		got, err := normalizeTZOffset(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got)
	}
	for _, bad := range []string{"0700", "+07000", "+ab00", "+99:00", ""} {
		_, err := normalizeTZOffset(bad)
		assert.Error(t, err, "tz_offset %q should be rejected", bad)
	}
}

func TestNormalizeStartTime(t *testing.T) {
	got, err := normalizeStartTime("08:00")
	require.NoError(t, err)
	assert.Equal(t, "08:00:00", got)

	got, err = normalizeStartTime("8:30")
	require.NoError(t, err)
	assert.Equal(t, "08:30:00", got)

	got, err = normalizeStartTime("08:00:15")
	require.NoError(t, err)
	assert.Equal(t, "08:00:15", got)

	for _, bad := range []string{"", "8", "25:00", "08:60", "aa:00"} {
		_, err := normalizeStartTime(bad)
		assert.Error(t, err, "start_time %q should be rejected", bad)
	}
}

func TestParseTimeSpent(t *testing.T) {
	cases := map[string]int{"8h": 28800, "1h 30m": 5400, "45m": 2700, "3600": 3600, "2h15m": 8100}
	for in, want := range cases {
		got, err := parseTimeSpent(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "abc", "0", "-5"} {
		_, err := parseTimeSpent(bad)
		assert.Error(t, err, "time_spent %q should be rejected", bad)
	}
}

func TestWorklogSeconds(t *testing.T) {
	secs, err := worklogSeconds(28800, "")
	require.NoError(t, err)
	assert.Equal(t, 28800, secs)

	secs, err = worklogSeconds(0, "8h")
	require.NoError(t, err)
	assert.Equal(t, 28800, secs)

	_, err = worklogSeconds(0, "")
	assert.Error(t, err)

	_, err = worklogSeconds(1, "1h")
	assert.Error(t, err)
}

// --- planWorklogDays: 2026-07-18 Sat, 2026-07-19 Sun, 2026-07-20 Mon ---

func TestPlanWorklogDays_SkipsWeekends(t *testing.T) {
	plans, err := planWorklogDays("2026-07-18", "2026-07-22", "08:00", "+0700", 28800, true, nil)
	require.NoError(t, err)
	require.Len(t, plans, 3)
	assert.Equal(t, "2026-07-20", plans[0].Date)
	assert.Equal(t, "2026-07-20T08:00:00.000+0700", plans[0].Started)
	assert.Equal(t, "2026-07-21", plans[1].Date)
	assert.Equal(t, "2026-07-22", plans[2].Date)
}

func TestPlanWorklogDays_KeepsWeekendsWhenAsked(t *testing.T) {
	plans, err := planWorklogDays("2026-07-18", "2026-07-19", "08:00", "+0700", 28800, false, nil)
	require.NoError(t, err)
	require.Len(t, plans, 2)
}

func TestPlanWorklogDays_ExistingDates(t *testing.T) {
	plans, err := planWorklogDays("2026-07-20", "2026-07-21", "08:00", "+0700", 28800, true, map[string]bool{"2026-07-20": true})
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.True(t, plans[0].Skip)
	assert.False(t, plans[1].Skip)
}

func TestPlanWorklogDays_Reversed(t *testing.T) {
	_, err := planWorklogDays("2026-07-21", "2026-07-20", "08:00", "+0700", 28800, true, nil)
	assert.Error(t, err)
}

func TestPlanWorklogDays_BadInputs(t *testing.T) {
	cases := []struct {
		name                string
		from, to, start, tz string
	}{
		{"bad from", "20-07-2026", "2026-07-21", "08:00", "+0700"},
		{"bad to", "2026-07-20", "21-07-2026", "08:00", "+0700"},
		{"bad start", "2026-07-20", "2026-07-21", "25:00", "+0700"},
		{"bad tz", "2026-07-20", "2026-07-21", "08:00", "0700"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planWorklogDays(tc.from, tc.to, tc.start, tc.tz, 28800, true, nil)
			assert.Error(t, err)
		})
	}
}

// --- writeWorklog via handlers ---

func TestWriteWorklog_Batch(t *testing.T) {
	var starteds []string
	mc := &mockClient{
		AddWorklogFn: func(_ context.Context, _, started string, seconds int, comment string) (string, error) {
			starteds = append(starteds, started)
			assert.Equal(t, 28800, seconds)
			assert.Equal(t, "Log tu dong", comment)
			return "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{FromDate: "2026-07-20", ToDate: "2026-07-21", TZOffset: "+0700", Comment: "Log tu dong"},
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "2 logged, 0 skipped")
	assert.Equal(t, []string{"2026-07-20T08:00:00.000+0700", "2026-07-21T08:00:00.000+0700"}, starteds)
}

func TestWriteWorklog_Batch_HoursPerDay(t *testing.T) {
	var gotSeconds int
	mc := &mockClient{
		AddWorklogFn: func(_ context.Context, _, _ string, seconds int, _ string) (string, error) {
			gotSeconds = seconds
			return "1", nil
		},
	}
	h := newWriteHandlers(mc)
	_, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{FromDate: "2026-07-20", ToDate: "2026-07-20", HoursPerDay: 4},
		}},
	})
	assert.False(t, isErr)
	assert.Equal(t, 14400, gotSeconds)
}

func TestWriteWorklog_SkipExisting(t *testing.T) {
	var logged int
	mc := &mockClient{
		GetWorklogsFn: func(context.Context, string) ([]jira.WorklogEntry, error) {
			return []jira.WorklogEntry{{ID: "9", Started: "2026-07-20T09:00:00.000+0700", TimeSpentSeconds: 3600}}, nil
		},
		AddWorklogFn: func(context.Context, string, string, int, string) (string, error) {
			logged++
			return "1", nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{FromDate: "2026-07-20", ToDate: "2026-07-21", TZOffset: "+0700", SkipExisting: true},
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "1 logged, 1 skipped")
	assert.Contains(t, text, "2026-07-20 — SKIP")
	assert.EqualValues(t, 1, logged)
}

func TestWriteWorklog_SingleEntry(t *testing.T) {
	var gotStarted string
	var gotSeconds int
	mc := &mockClient{
		AddWorklogFn: func(_ context.Context, _, started string, seconds int, _ string) (string, error) {
			gotStarted, gotSeconds = started, seconds
			return "7", nil
		},
	}
	h := newWriteHandlers(mc)
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{Started: "2026-07-20", TimeSpent: "8h", TZOffset: "+0700"},
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "worklog_id=7")
	assert.Equal(t, "2026-07-20T08:00:00.000+0700", gotStarted)
	assert.Equal(t, 28800, gotSeconds)
}

func TestWriteWorklog_SingleEntry_FullTimestamp(t *testing.T) {
	var gotStarted string
	mc := &mockClient{
		AddWorklogFn: func(_ context.Context, _, started string, _ int, _ string) (string, error) {
			gotStarted = started
			return "7", nil
		},
	}
	h := newWriteHandlers(mc)
	_, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{Started: "2026-07-20T08:00:00.000+0700", TimeSpentSeconds: 28800},
		}},
	})
	assert.False(t, isErr)
	assert.Equal(t, "2026-07-20T08:00:00.000+0700", gotStarted)
}

func TestWriteWorklog_DryRun(t *testing.T) {
	h := newWriteHandlers(&mockClient{})
	text, isErr := callWrite(t, h, WriteArgs{
		Action: "worklog",
		DryRun: true,
		Items: []WriteItem{{
			Key:     "PROJ-1",
			Worklog: &WorklogItem{FromDate: "2026-07-20", ToDate: "2026-07-21"},
		}},
	})
	assert.False(t, isErr)
	assert.Contains(t, text, "Would log 2 worklog entries")
}

func TestWriteWorklog_ValidationErrors(t *testing.T) {
	cases := []struct {
		name  string
		item  WriteItem
		match string
	}{
		{"missing key", WriteItem{Worklog: &WorklogItem{FromDate: "2026-07-20", ToDate: "2026-07-21"}}, "worklog requires key"},
		{"missing payload", WriteItem{Key: "PROJ-1"}, "worklog requires a worklog payload"},
		{"both modes", WriteItem{Key: "PROJ-1", Worklog: &WorklogItem{Started: "2026-07-20", FromDate: "2026-07-20", ToDate: "2026-07-21"}}, "not both"},
		{"no time in single", WriteItem{Key: "PROJ-1", Worklog: &WorklogItem{Started: "2026-07-20"}}, "time is required"},
		{"missing to_date", WriteItem{Key: "PROJ-1", Worklog: &WorklogItem{FromDate: "2026-07-20"}}, "requires to_date"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHandlers(&mockClient{})
			text, _ := callWrite(t, h, WriteArgs{Action: "worklog", Items: []WriteItem{tc.item}})
			assert.Contains(t, text, "ERROR")
			assert.Contains(t, text, tc.match)
		})
	}
}

// --- readWorklogs via handlers ---

func TestReadWorklogs(t *testing.T) {
	mc := &mockClient{
		GetWorklogsFn: func(_ context.Context, issueKey string) ([]jira.WorklogEntry, error) {
			assert.Equal(t, "PROJ-1", issueKey)
			return []jira.WorklogEntry{
				{ID: "1", Started: "2026-07-20T08:00:00.000+0700", TimeSpentSeconds: 28800, AuthorName: "Dev A"},
				{ID: "2", Started: "2026-07-21T08:00:00.000+0700", TimeSpentSeconds: 28800},
			}, nil
		},
	}
	h := &handlers{client: mc}
	result := h.readWorklogs(context.Background(), ReadArgs{IssueKey: "PROJ-1"})
	text := result.Content[0].(*mcp.TextContent).Text
	assert.Contains(t, text, "Found 2 worklog entries on PROJ-1 (sum 57600s)")
	assert.Contains(t, text, "Dev A")
}

func TestReadWorklogs_RequiresIssueKey(t *testing.T) {
	h := &handlers{client: &mockClient{}}
	result := h.readWorklogs(context.Background(), ReadArgs{})
	text := result.Content[0].(*mcp.TextContent).Text
	assert.True(t, result.IsError)
	assert.Contains(t, text, "issue_key is required")
}
