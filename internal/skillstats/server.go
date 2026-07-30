package skillstats

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

//go:embed dashboard.html
var assets embed.FS

// rescanInterval is how stale the store may be before a dashboard request
// triggers a fresh transcript scan. Incremental scans are cheap, but a scan per
// request would still restat every transcript on every poll.
const rescanInterval = 20 * time.Second

// Server serves the dashboard page and its JSON API over a Store.
type Server struct {
	store *Store

	// scanMu serializes rescans so concurrent requests (the page issues
	// several) trigger one scan rather than one each.
	scanMu sync.Mutex
}

// NewServer wraps a store in the HTTP handlers.
func NewServer(store *Store) *Server { return &Server{store: store} }

// Handler returns the routed handler for the dashboard.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handlePage)
	mux.HandleFunc("GET /api/stats", s.handleStats)
	mux.HandleFunc("GET /api/meta", s.handleMeta)
	mux.HandleFunc("POST /api/scan", s.handleScan)
	return mux
}

// handlePage serves the single-page dashboard.
func (s *Server) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	page, err := assets.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "dashboard asset missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}

// handleStats returns one bucketed series. Granularity picks its own default
// window, so the page can ask for "week" and get a sensible quarter of history
// without encoding that policy in the browser.
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.maybeScan()

	gran := ParseGranularity(r.URL.Query().Get("gran"))
	buckets := 0
	if raw := r.URL.Query().Get("buckets"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			buckets = n
		}
	}
	stats := Compute(s.store.Events(), Query{
		Gran:    gran,
		Buckets: buckets,
		Profile: r.URL.Query().Get("profile"),
	})
	writeJSON(w, stats)
}

// metaResponse describes what the dashboard can filter by.
type metaResponse struct {
	Profiles []string  `json:"profiles"`
	Defaults []granDef `json:"defaults"`
	LastScan time.Time `json:"lastScan"`
	Total    int       `json:"total"`
	// RetentionMonths lets the page state the real retention window instead of
	// hard-coding a number that would silently drift from the constant.
	RetentionMonths int `json:"retentionMonths"`
}

// granDef advertises a granularity and its default window to the page.
type granDef struct {
	Gran    Granularity `json:"gran"`
	Buckets int         `json:"buckets"`
}

// handleMeta returns the profile list and per-granularity defaults.
func (s *Server) handleMeta(w http.ResponseWriter, _ *http.Request) {
	s.maybeScan()
	writeJSON(w, metaResponse{
		Profiles: s.store.Profiles(),
		Defaults: []granDef{
			{GranHour, DefaultBuckets(GranHour)},
			{GranDay, DefaultBuckets(GranDay)},
			{GranWeek, DefaultBuckets(GranWeek)},
		},
		LastScan:        s.store.LastScan(),
		Total:           len(s.store.Events()),
		RetentionMonths: RetentionMonths,
	})
}

// handleScan forces a scan regardless of staleness.
func (s *Server) handleScan(w http.ResponseWriter, _ *http.Request) {
	s.scanMu.Lock()
	result := s.store.Scan()
	s.scanMu.Unlock()
	writeJSON(w, result)
}

// maybeScan refreshes the store if it has gone stale.
func (s *Server) maybeScan() {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	if time.Since(s.store.LastScan()) < rescanInterval {
		return
	}
	if result := s.store.Scan(); len(result.Errors) > 0 {
		log.Printf("skillstats: scan reported %d errors, first: %s", len(result.Errors), result.Errors[0])
	}
}

// writeJSON encodes a response body, logging rather than partially rewriting a
// response whose header is already committed.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("skillstats: encode response: %v", err)
	}
}
