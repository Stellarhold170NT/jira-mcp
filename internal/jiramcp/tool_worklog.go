package jiramcp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// jiraDateRe matches a bare YYYY-MM-DD calendar date.
var jiraDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

var (
	timeSpentHoursRe   = regexp.MustCompile(`(\d+)\s*h`)
	timeSpentMinutesRe = regexp.MustCompile(`(\d+)\s*m`)
)

// worklogPlan is one scheduled work-log entry produced by planWorklogDays.
type worklogPlan struct {
	Date    string // YYYY-MM-DD calendar date the entry belongs to
	Started string // full Jira started timestamp
	Seconds int
	Skip    bool // day already has an entry (skip_existing)
}

// normalizeTZOffset accepts +0700 / +07:00 / -0530 / -05:30 and returns the
// Jira form +0700 (sign plus four digits, no colon).
func normalizeTZOffset(s string) (string, error) {
	t := strings.ReplaceAll(strings.TrimSpace(s), ":", "")
	if len(t) != 5 || (t[0] != '+' && t[0] != '-') {
		return "", fmt.Errorf("invalid tz_offset %q: use a UTC offset like +0700 or +07:00", s)
	}
	hh, err1 := strconv.Atoi(t[1:3])
	mm, err2 := strconv.Atoi(t[3:5])
	if err1 != nil || err2 != nil || hh > 14 || mm > 59 {
		return "", fmt.Errorf("invalid tz_offset %q: use a UTC offset like +0700 or +07:00", s)
	}
	return t, nil
}

// normalizeStartTime accepts HH:MM or HH:MM:SS (24-hour clock) and returns
// HH:MM:SS.
func normalizeStartTime(s string) (string, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 && len(parts) != 3 {
		return "", fmt.Errorf("invalid start_time %q: use HH:MM or HH:MM:SS", s)
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return "", fmt.Errorf("invalid start_time %q: use HH:MM or HH:MM:SS", s)
		}
		nums[i] = n
	}
	if nums[0] > 23 || nums[1] > 59 || (len(parts) == 3 && nums[2] > 59) {
		return "", fmt.Errorf("invalid start_time %q: hour must be 00-23, minutes/seconds 00-59", s)
	}
	if len(parts) == 2 {
		return fmt.Sprintf("%02d:%02d:00", nums[0], nums[1]), nil
	}
	return fmt.Sprintf("%02d:%02d:%02d", nums[0], nums[1], nums[2]), nil
}

// parseTimeSpent parses a Jira-style duration ("8h", "1h 30m", "45m") into
// seconds. A plain integer is treated as seconds.
func parseTimeSpent(s string) (int, error) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if n, err := strconv.Atoi(t); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("invalid time_spent %q: must be positive", s)
		}
		return n, nil
	}
	hours, minutes := 0, 0
	if m := timeSpentHoursRe.FindStringSubmatch(t); m != nil {
		hours, _ = strconv.Atoi(m[1])
	}
	if m := timeSpentMinutesRe.FindStringSubmatch(t); m != nil {
		minutes, _ = strconv.Atoi(m[1])
	}
	if hours == 0 && minutes == 0 {
		return 0, fmt.Errorf("invalid time_spent %q: use a Jira duration like 8h or 1h 30m", s)
	}
	return hours*3600 + minutes*60, nil
}

// worklogSeconds resolves a single-entry duration: explicit seconds win, then
// the duration string. Exactly one source must be present.
func worklogSeconds(explicit int, duration string) (int, error) {
	switch {
	case explicit > 0 && duration != "":
		return 0, fmt.Errorf("worklog: pass either time_spent_seconds or time_spent, not both")
	case explicit > 0:
		return explicit, nil
	case duration != "":
		return parseTimeSpent(duration)
	default:
		return 0, fmt.Errorf("worklog: time is required — set time_spent_seconds or time_spent")
	}
}

// planWorklogDays builds the deterministic day plan for batch worklogging:
// one entry per calendar day from fromDate to toDate inclusive, weekends
// skipped when skipWeekends, each starting at startTime in tzOffset.
// existingDates is keyed by YYYY-MM-DD (prefix of an existing entry's started
// timestamp).
func planWorklogDays(fromDate, toDate, startTime, tzOffset string, secondsPerDay int, skipWeekends bool, existingDates map[string]bool) ([]worklogPlan, error) {
	if !jiraDateRe.MatchString(fromDate) {
		return nil, fmt.Errorf("from_date %q must be YYYY-MM-DD", fromDate)
	}
	if !jiraDateRe.MatchString(toDate) {
		return nil, fmt.Errorf("to_date %q must be YYYY-MM-DD", toDate)
	}
	if startTime == "" {
		startTime = "08:00"
	}
	if tzOffset == "" {
		tzOffset = "+0000"
	}
	start, err := normalizeStartTime(startTime)
	if err != nil {
		return nil, err
	}
	tz, err := normalizeTZOffset(tzOffset)
	if err != nil {
		return nil, err
	}
	from, err := time.ParseInLocation("2006-01-02", fromDate, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("invalid from_date %q: %w", fromDate, err)
	}
	to, err := time.ParseInLocation("2006-01-02", toDate, time.UTC)
	if err != nil {
		return nil, fmt.Errorf("invalid to_date %q: %w", toDate, err)
	}
	if to.Before(from) {
		return nil, fmt.Errorf("to_date %s is before from_date %s", toDate, fromDate)
	}
	if days := int(to.Sub(from).Hours()/24) + 1; days > 400 {
		return nil, fmt.Errorf("date range spans %d days — cap is 400; split the range", days)
	}

	var plans []worklogPlan
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		wd := d.Weekday()
		if skipWeekends && (wd == time.Saturday || wd == time.Sunday) {
			continue
		}
		date := d.Format("2006-01-02")
		plans = append(plans, worklogPlan{
			Date:    date,
			Started: fmt.Sprintf("%sT%s.000%s", date, start, tz),
			Seconds: secondsPerDay,
			Skip:    existingDates[date],
		})
	}
	return plans, nil
}

// buildCommentBodyForAPI resolves the comment body for the API version in
// use. On v2 the raw string goes verbatim (legacy wiki-markup/plain text) and
// format=wiki routes to the v2 comment endpoints.
func (h *handlers) buildCommentBodyForAPI(body, rawFormat string) (any, string, error) {
	if h.client.APIVersion() == 2 {
		return body, formatWiki, nil
	}
	return buildCommentBody(body, rawFormat)
}

// writeWorklog handles action=worklog. The batch day loop is computed
// server-side (deterministic) rather than by the model: the model only picks
// the range, hours, and clock time.
func (h *handlers) writeWorklog(ctx context.Context, item WriteItem, dryRun bool) (string, error) {
	if item.Key == "" {
		return "", fmt.Errorf("worklog requires key")
	}
	wl := item.Worklog
	if wl == nil {
		return "", fmt.Errorf("worklog requires a worklog payload")
	}
	if wl.Started != "" && wl.FromDate != "" {
		return "", fmt.Errorf("worklog: pass either started (single entry) or from_date/to_date (batch), not both")
	}
	if wl.Started == "" && wl.FromDate == "" {
		return "", fmt.Errorf("worklog: single entry needs started + time_spent_seconds (or time_spent); batch needs from_date + to_date")
	}

	comment := strings.TrimSpace(wl.Comment)

	// Single-entry mode.
	if wl.Started != "" {
		seconds, err := worklogSeconds(wl.TimeSpentSeconds, wl.TimeSpent)
		if err != nil {
			return "", err
		}
		started := wl.Started
		if jiraDateRe.MatchString(started) {
			startTime := wl.StartTime
			if startTime == "" {
				startTime = "08:00"
			}
			tzOffset := wl.TZOffset
			if tzOffset == "" {
				tzOffset = "+0000"
			}
			start, err := normalizeStartTime(startTime)
			if err != nil {
				return "", err
			}
			tz, err := normalizeTZOffset(tzOffset)
			if err != nil {
				return "", err
			}
			started = fmt.Sprintf("%sT%s.000%s", started, start, tz)
		}
		if dryRun {
			return fmt.Sprintf("Would log %ds on %s starting %s.", seconds, item.Key, started), nil
		}
		id, err := h.client.AddWorklog(ctx, item.Key, started, seconds, comment)
		if err != nil {
			return "", fmt.Errorf("failed to add worklog to %s: %w", item.Key, err)
		}
		return fmt.Sprintf("Logged %ds on %s starting %s (worklog_id=%s).", seconds, item.Key, started, id), nil
	}

	// Batch mode.
	if wl.ToDate == "" {
		return "", fmt.Errorf("worklog batch mode requires to_date alongside from_date")
	}
	seconds := 0
	switch {
	case wl.TimeSpent != "":
		var err error
		if seconds, err = parseTimeSpent(wl.TimeSpent); err != nil {
			return "", err
		}
	case wl.HoursPerDay > 0:
		seconds = wl.HoursPerDay * 3600
	default:
		seconds = 8 * 3600
	}

	existing := map[string]bool{}
	if wl.SkipExisting {
		entries, err := h.client.GetWorklogs(ctx, item.Key)
		if err != nil {
			return "", fmt.Errorf("failed to read existing worklogs on %s: %w", item.Key, err)
		}
		for _, e := range entries {
			if len(e.Started) >= 10 {
				existing[e.Started[:10]] = true
			}
		}
	}

	skipWeekends := true
	if wl.SkipWeekends != nil {
		skipWeekends = *wl.SkipWeekends
	}
	plans, err := planWorklogDays(wl.FromDate, wl.ToDate, wl.StartTime, wl.TZOffset, seconds, skipWeekends, existing)
	if err != nil {
		return "", err
	}
	if len(plans) == 0 {
		return fmt.Sprintf("No working days between %s and %s — nothing to log on %s.", wl.FromDate, wl.ToDate, item.Key), nil
	}

	if dryRun {
		var b strings.Builder
		fmt.Fprintf(&b, "Would log %d worklog entries on %s (%ds per day):", len(plans), item.Key, seconds)
		for _, p := range plans {
			if p.Skip {
				fmt.Fprintf(&b, "\n  %s — SKIP (already logged)", p.Date)
			} else {
				fmt.Fprintf(&b, "\n  %s — log %ds starting %s", p.Date, p.Seconds, p.Started)
			}
		}
		return b.String(), nil
	}

	var logged, skipped int
	var b strings.Builder
	for _, p := range plans {
		if p.Skip {
			skipped++
			fmt.Fprintf(&b, "\n  %s — SKIP (already logged)", p.Date)
			continue
		}
		if _, err := h.client.AddWorklog(ctx, item.Key, p.Started, p.Seconds, comment); err != nil {
			fmt.Fprintf(&b, "\n  %s — ERROR: %v", p.Date, err)
			continue
		}
		logged++
		fmt.Fprintf(&b, "\n  %s — logged %ds (started %s)", p.Date, p.Seconds, p.Started)
	}
	return fmt.Sprintf("Worklog on %s: %d logged, %d skipped (range %s..%s, %ds per day).%s",
		item.Key, logged, skipped, wl.FromDate, wl.ToDate, seconds, b.String()), nil
}

// readWorklogs handles jira_read resource=worklogs.
func (h *handlers) readWorklogs(ctx context.Context, args ReadArgs) *mcp.CallToolResult {
	if args.IssueKey == "" {
		return textResult("issue_key is required for resource=worklogs. Hint: pass the issue key whose worklogs you want, e.g. {\"resource\": \"worklogs\", \"issue_key\": \"PROJ-1\"}.", true)
	}
	entries, err := h.client.GetWorklogs(ctx, args.IssueKey)
	if err != nil {
		return textResult(fmt.Sprintf("Failed to get worklogs for %s: %v", args.IssueKey, err), true)
	}
	var results []map[string]any
	sum := 0
	for _, e := range entries {
		sum += e.TimeSpentSeconds
		m := map[string]any{
			"id":                 e.ID,
			"started":            e.Started,
			"time_spent_seconds": e.TimeSpentSeconds,
		}
		if e.AuthorName != "" {
			m["author"] = e.AuthorName
		}
		results = append(results, m)
	}
	return formatReadResult(fmt.Sprintf("Found %d worklog entries on %s (sum %ds)", len(results), args.IssueKey, sum), results, nil)
}
