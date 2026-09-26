package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	apiv1 "github.com/mcihad/kentos-izci/gen/izci/api/v1"
	"github.com/mcihad/kentos-izci/internal/search"
)

// sqlb accumulates WHERE conditions and positional arguments.
type sqlb struct {
	where []string
	args  []any
}

func (b *sqlb) arg(v any) string {
	b.args = append(b.args, v)
	return "$" + strconv.Itoa(len(b.args))
}

func (b *sqlb) add(cond string) { b.where = append(b.where, cond) }

func (b *sqlb) clause() string {
	if len(b.where) == 0 {
		return "true"
	}
	return strings.Join(b.where, " AND ")
}

// tagKeys maps search keys to issue_tags keys.
var tagKeys = map[string]string{
	"environment": "environment", "release": "release", "customer": "customer", "tenant": "tenant",
	"user": "user", "browser": "browser", "os": "os", "device": "device", "server": "server_name",
	"runtime": "runtime", "transaction": "transaction", "logger": "logger", "handled": "handled",
}

var levelNames = map[string]int16{"debug": 1, "info": 2, "warning": 3, "warn": 3, "error": 4, "fatal": 5}

var priorityNames = map[string]int16{"low": 1, "medium": 2, "high": 3, "critical": 4}

func queryError(msg string, pos int, token string) error {
	return apiError(connect.CodeInvalidArgument, apiv1.ErrorReason_ERROR_REASON_INVALID_QUERY, msg,
		"position", strconv.Itoa(pos), "token", token)
}

// issueFilter translates a search query into conditions on issues AS i.
func issueFilter(b *sqlb, q *search.Query, orgID, userID uuid.UUID, now time.Time) error {
	for _, t := range q.Terms {
		cond, err := issueTerm(b, t, orgID, userID, now)
		if err != nil {
			return err
		}
		if t.Negate {
			cond = "NOT (" + cond + ")"
		}
		b.add(cond)
	}
	for _, text := range q.Text {
		p := b.arg("%" + likeEscape(text) + "%")
		b.add("(i.title ILIKE " + p + " OR i.culprit ILIKE " + p + ")")
	}
	return nil
}

func issueTerm(b *sqlb, t search.Term, orgID, userID uuid.UUID, now time.Time) (string, error) {
	switch t.Key {
	case "is":
		var parts []string
		for _, v := range t.Values {
			switch strings.ToLower(v) {
			case "unresolved":
				parts = append(parts, "i.status = 1")
			case "resolved":
				parts = append(parts, "i.status = 2")
			case "ignored":
				parts = append(parts, "i.status = 3")
			case "new":
				parts = append(parts, "i.first_seen >= "+b.arg(now.Add(-72*time.Hour)))
			case "regressed":
				parts = append(parts, "i.substatus = 3")
			case "escalating":
				parts = append(parts, "i.substatus = 4")
			case "assigned":
				parts = append(parts, "i.assignee_id IS NOT NULL")
			case "unassigned":
				parts = append(parts, "i.assignee_id IS NULL")
			case "unhandled":
				parts = append(parts, "i.unhandled")
			default:
				return "", queryError("unknown is: value", t.Pos, v)
			}
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	case "level", "priority":
		names := levelNames
		col := "i.level"
		if t.Key == "priority" {
			names, col = priorityNames, "i.priority"
		}
		var vals []int16
		for _, v := range t.Values {
			n, ok := names[strings.ToLower(v)]
			if !ok {
				return "", queryError("unknown "+t.Key, t.Pos, v)
			}
			vals = append(vals, n)
		}
		if t.Op != search.OpEq {
			return col + " " + string(t.Op) + " " + b.arg(vals[0]), nil
		}
		return col + " = ANY(" + b.arg(vals) + ")", nil
	case "project":
		return "i.project_id IN (SELECT id FROM projects WHERE org_id = " + b.arg(orgID) + " AND slug = ANY(" + b.arg(lower(t.Values)) + "))", nil
	case "assigned":
		v := strings.ToLower(t.Values[0])
		switch v {
		case "me":
			return "i.assignee_id = " + b.arg(userID), nil
		case "none", "nobody":
			return "i.assignee_id IS NULL", nil
		}
		return "i.assignee_id IN (SELECT id FROM users WHERE lower(email) = ANY(" + b.arg(lower(t.Values)) + "))", nil
	case "type":
		return anyLike(b, "i.type", t.Values), nil
	case "file":
		return anyLike(b, "(i.metadata->>'filename')", wrapStars(t.Values)), nil
	case "has":
		var parts []string
		for _, v := range t.Values {
			switch strings.ToLower(v) {
			case "ai":
				parts = append(parts, "i.ai_state > 1")
			case "user", "users":
				parts = append(parts, "i.users_count > 0")
			case "customer", "customers":
				parts = append(parts, "i.customers_count > 0")
			case "assignee":
				parts = append(parts, "i.assignee_id IS NOT NULL")
			default:
				return "", queryError("unknown has: value", t.Pos, v)
			}
		}
		return "(" + strings.Join(parts, " OR ") + ")", nil
	case "first_seen", "last_seen":
		return timeCondition(b, "i."+t.Key, t, now)
	case "times_seen", "users", "customers":
		col := map[string]string{"times_seen": "i.times_seen", "users": "i.users_count", "customers": "i.customers_count"}[t.Key]
		n, err := strconv.ParseInt(t.Values[0], 10, 64)
		if err != nil {
			return "", queryError("expected a number", t.Pos, t.Values[0])
		}
		op := string(t.Op)
		if op == "" {
			op = "="
		}
		return col + " " + op + " " + b.arg(n), nil
	}
	key := t.Key
	if k, ok := tagKeys[key]; ok {
		key = k
	}
	values := t.Values
	if key == "customer" {
		values = lower(values)
	}
	valueCond := "t.value = ANY(" + b.arg(values) + ")"
	if hasStar(values) {
		valueCond = anyLike(b, "t.value", values)
	}
	return "EXISTS (SELECT 1 FROM issue_tags t WHERE t.issue_id = i.id AND t.key = " + b.arg(key) + " AND " + valueCond + ")", nil
}

// timeCondition handles -24h (within), +7d (older than), and absolute
// dates with operators.
func timeCondition(b *sqlb, col string, t search.Term, now time.Time) (string, error) {
	v := t.Values[0]
	if strings.HasPrefix(v, "-") || strings.HasPrefix(v, "+") {
		d, err := parseAge(v[1:])
		if err != nil {
			return "", queryError("expected a duration like -24h or +7d", t.Pos, v)
		}
		if v[0] == '-' {
			return col + " >= " + b.arg(now.Add(-d)), nil
		}
		return col + " < " + b.arg(now.Add(-d)), nil
	}
	day, err := time.ParseInLocation("2006-01-02", v, time.UTC)
	if err != nil {
		ts, err2 := time.Parse(time.RFC3339, v)
		if err2 != nil {
			return "", queryError("expected a date (YYYY-MM-DD) or duration", t.Pos, v)
		}
		day = ts
	}
	switch t.Op {
	case search.OpGt:
		return col + " >= " + b.arg(day.AddDate(0, 0, 1)), nil
	case search.OpGe:
		return col + " >= " + b.arg(day), nil
	case search.OpLt:
		return col + " < " + b.arg(day), nil
	case search.OpLe:
		return col + " < " + b.arg(day.AddDate(0, 0, 1)), nil
	}
	return "(" + col + " >= " + b.arg(day) + " AND " + col + " < " + b.arg(day.AddDate(0, 0, 1)) + ")", nil
}

// parseAge parses 30m, 24h, 7d, 2w.
func parseAge(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("bad duration")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad duration")
	}
	switch s[len(s)-1] {
	case 'm':
		return time.Duration(n) * time.Minute, nil
	case 'h':
		return time.Duration(n) * time.Hour, nil
	case 'd':
		return time.Duration(n) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("bad duration")
}

func anyLike(b *sqlb, col string, values []string) string {
	pats := make([]string, len(values))
	for i, v := range values {
		pats[i] = strings.ReplaceAll(likeEscape(v), "*", "%")
	}
	return col + " ILIKE ANY(" + b.arg(pats) + ")"
}

func wrapStars(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = "*" + v + "*"
	}
	return out
}

func hasStar(values []string) bool {
	for _, v := range values {
		if strings.Contains(v, "*") {
			return true
		}
	}
	return false
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

func lower(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.ToLower(v)
	}
	return out
}
