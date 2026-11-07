// pool-usage serves GET /v1/pool-usage for a sub2api deployment.
//
// A client authenticates with its normal sub2api API key (x-api-key or
// Authorization: Bearer). The key's group decides which upstream accounts are
// counted, and the response reports how much of the combined 5-hour and 7-day
// allowance of those accounts is left. It only reads sub2api's database.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type window struct {
	RemainingPercent float64    `json:"remaining_percent"`
	NextResetAt      *time.Time `json:"next_reset_at,omitempty"`
}

// Only pool totals are returned; individual accounts are never exposed.
type poolResponse struct {
	Group     string    `json:"group"`
	FiveHour  window    `json:"five_hour"`
	SevenDay  window    `json:"seven_day"`
	Text      string    `json:"text"`
	Generated time.Time `json:"generated_at"`
	accounts  int
}

type cached struct {
	at   time.Time
	resp poolResponse
}

type server struct {
	db    *pgxpool.Pool
	ttl   time.Duration
	mu    sync.Mutex
	cache map[int64]cached
}

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("parse DATABASE_URL: %v", err)
	}
	// The password lives in a mounted file so it never appears in compose env.
	if pwFile := os.Getenv("DATABASE_PASSWORD_FILE"); pwFile != "" {
		b, err := os.ReadFile(pwFile)
		if err != nil {
			log.Fatalf("read password file: %v", err)
		}
		poolCfg.ConnConfig.Password = strings.TrimSpace(string(b))
	}
	poolCfg.MaxConns = 4
	db, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	run(db)
}

func run(db *pgxpool.Pool) {
	ttl := 15 * time.Second
	if v, err := strconv.Atoi(os.Getenv("CACHE_SECONDS")); err == nil && v >= 0 {
		ttl = time.Duration(v) * time.Second
	}
	s := &server{db: db, ttl: ttl, cache: map[int64]cached{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/pool-usage", s.handle)
	mux.HandleFunc("/v1/pool-usage/healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	log.Printf("pool-usage listening on %s (cache %s)", addr, ttl)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func clientKey(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("x-api-key")); k != "" {
		return k
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, typ, msg string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]string{"type": typ, "message": msg}})
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "invalid_request_error", "use GET")
		return
	}
	key := clientKey(r)
	if key == "" {
		writeErr(w, http.StatusUnauthorized, "authentication_error", "missing API key")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var groupID int64
	var groupName string
	err := s.db.QueryRow(ctx, `
		SELECT g.id, g.name
		FROM api_keys k
		JOIN users u ON u.id = k.user_id
		JOIN groups g ON g.id = k.group_id
		WHERE k.key = $1
		  AND k.deleted_at IS NULL AND k.status = 'active'
		  AND (k.expires_at IS NULL OR k.expires_at > now())
		  AND u.deleted_at IS NULL AND u.status = 'active'
		  AND g.deleted_at IS NULL`, key).Scan(&groupID, &groupName)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErr(w, http.StatusUnauthorized, "authentication_error", "invalid API key")
		return
	}
	if err != nil {
		log.Printf("key lookup: %v", err)
		writeErr(w, http.StatusInternalServerError, "api_error", "lookup failed")
		return
	}

	s.mu.Lock()
	if c, ok := s.cache[groupID]; ok && time.Since(c.at) < s.ttl {
		s.mu.Unlock()
		respond(w, r, c.resp)
		return
	}
	s.mu.Unlock()

	resp, err := s.build(ctx, groupID, groupName)
	if err != nil {
		log.Printf("build group %d: %v", groupID, err)
		writeErr(w, http.StatusInternalServerError, "api_error", "usage unavailable")
		return
	}
	s.mu.Lock()
	s.cache[groupID] = cached{at: time.Now(), resp: resp}
	s.mu.Unlock()
	respond(w, r, resp)
}

func respond(w http.ResponseWriter, r *http.Request, resp poolResponse) {
	switch r.URL.Query().Get("format") {
	case "text":
		writePlain(w, resp.Text)
	case "full":
		writePlain(w, renderFull(resp, time.Now().UTC()))
	case "hud":
		writeJSON(w, http.StatusOK, hudSnapshot(resp))
	default:
		writeJSON(w, http.StatusOK, resp)
	}
}

func writePlain(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintln(w, s)
}

func parseFloat(s *string) (float64, bool) {
	if s == nil || strings.TrimSpace(*s) == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(*s), 64)
	if err != nil || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

func parseUnix(s *string) *time.Time {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
		t := time.Unix(n, 0).UTC()
		return &t
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		t = t.UTC()
		return &t
	}
	return nil
}

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }

func (s *server) build(ctx context.Context, groupID int64, groupName string) (poolResponse, error) {
	rows, err := s.db.Query(ctx, `
		SELECT a.id, a.status, a.schedulable,
		       a.rate_limit_reset_at, a.temp_unschedulable_until, a.overload_until,
		       a.session_window_end,
		       a.extra->>'session_window_utilization',
		       a.extra->>'passive_usage_7d_utilization',
		       a.extra->>'passive_usage_7d_reset'
		FROM accounts a
		JOIN account_groups ag ON ag.account_id = a.id
		WHERE ag.group_id = $1 AND a.deleted_at IS NULL
		  AND a.status = 'active' AND a.schedulable
		ORDER BY a.priority, a.id`, groupID)
	if err != nil {
		return poolResponse{}, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	resp := poolResponse{Group: groupName, Generated: now}
	var sum5, sum7 float64
	var next5, next7 *time.Time
	earliest := func(cur *time.Time, t *time.Time) *time.Time {
		if t == nil || !t.After(now) {
			return cur
		}
		if cur == nil || t.Before(*cur) {
			return t
		}
		return cur
	}

	for rows.Next() {
		var id int64
		var status string
		var schedulable bool
		var rlReset, tempUntil, overload, sessEnd *time.Time
		var u5s, u7s, r7s *string
		if err := rows.Scan(&id, &status, &schedulable, &rlReset, &tempUntil, &overload, &sessEnd, &u5s, &u7s, &r7s); err != nil {
			return poolResponse{}, err
		}
		resp.accounts++

		limited := rlReset != nil && rlReset.After(now)
		paused := (tempUntil != nil && tempUntil.After(now)) || (overload != nil && overload.After(now))

		// 5-hour window: utilization only counts while the window it was measured in is still open.
		// An account with no data yet counts as unused.
		u5, has5 := parseFloat(u5s)
		if has5 && sessEnd != nil && !sessEnd.After(now) {
			u5 = 0
		}
		// 7-day window, same rule.
		u7, has7 := parseFloat(u7s)
		r7 := parseUnix(r7s)
		if has7 && r7 != nil && !r7.After(now) {
			u7 = 0
		}
		rem5, rem7 := 1-clamp01(u5), 1-clamp01(u7)
		if limited {
			rem5 = 0
			next5 = earliest(next5, rlReset)
		} else if paused {
			rem5 = 0
		}
		sum5 += rem5
		sum7 += rem7

		if has5 && u5 > 0 && sessEnd != nil {
			t := sessEnd.UTC()
			next5 = earliest(next5, &t)
		}
		if has7 && u7 > 0 {
			next7 = earliest(next7, r7)
		}
	}
	if err := rows.Err(); err != nil {
		return poolResponse{}, err
	}

	if resp.accounts > 0 {
		n := float64(resp.accounts)
		resp.FiveHour = window{RemainingPercent: round1(sum5 / n * 100), NextResetAt: next5}
		resp.SevenDay = window{RemainingPercent: round1(sum7 / n * 100), NextResetAt: next7}
	}
	resp.Text = renderText(resp, now)
	return resp, nil
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }

func humanUntil(t *time.Time, now time.Time) string {
	if t == nil {
		return ""
	}
	d := t.Sub(now)
	if d <= 0 {
		return "now"
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

func renderText(resp poolResponse, now time.Time) string {
	if resp.accounts == 0 {
		return "pool: no accounts available"
	}
	part := func(name string, w window) string {
		s := fmt.Sprintf("%s %.0f%% left", name, w.RemainingPercent)
		if r := humanUntil(w.NextResetAt, now); r != "" {
			s += " (reset " + r + ")"
		}
		return s
	}
	return part("5h", resp.FiveHour) + " · " + part("7d", resp.SevenDay)
}

// hudSnapshot matches claude-hud's display.externalUsagePath file format, so
// claude-hud draws the pool as its own 5h / 7d usage bars. Pool "used" is
// 100 minus the remaining percentage across all accounts.
func hudSnapshot(resp poolResponse) map[string]any {
	win := func(w window) map[string]any {
		out := map[string]any{"used_percentage": nil, "resets_at": nil}
		if resp.accounts > 0 {
			out["used_percentage"] = round1(100 - w.RemainingPercent)
		}
		if w.NextResetAt != nil {
			out["resets_at"] = w.NextResetAt.Format(time.RFC3339)
		}
		return out
	}
	return map[string]any{
		"updated_at": resp.Generated.Format(time.RFC3339),
		"five_hour":  win(resp.FiveHour),
		"seven_day":  win(resp.SevenDay),
	}
}

// renderFull is the multi-line view used by the /pool-usage command.
func renderFull(resp poolResponse, now time.Time) string {
	if resp.accounts == 0 {
		return fmt.Sprintf("Pool %s has no available accounts.", resp.Group)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Pool usage · %s\n", resp.Group)
	line := func(name string, w window) {
		fmt.Fprintf(&b, "  %s  %5.1f%% left", name, w.RemainingPercent)
		if r := humanUntil(w.NextResetAt, now); r != "" {
			fmt.Fprintf(&b, "  · next reset in %s", r)
		}
		b.WriteString("\n")
	}
	line("5h", resp.FiveHour)
	line("7d", resp.SevenDay)
	return strings.TrimRight(b.String(), "\n")
}
