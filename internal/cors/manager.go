package cors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	maxOrigins     = 100
	maxOriginBytes = 512
)

var (
	ErrForbidden      = errors.New("only the instance owner can manage CORS origins")
	ErrTooManyOrigins = fmt.Errorf("at most %d CORS origins are allowed", maxOrigins)
	ErrInvalidOrigin  = errors.New("invalid CORS origin")
)

var builtInOrigins = []string{
	"http://wails.localhost",
	"https://wails.localhost",
}

type originSnapshot struct {
	list []string
	set  map[string]struct{}
}

// Manager owns the persistent CORS allow-list and an immutable in-memory
// snapshot used on every HTTP request. Replacing the list updates the snapshot
// immediately after the database transaction commits.
type Manager struct {
	db      *sql.DB
	updates sync.Mutex
	origins atomic.Pointer[originSnapshot]
}

func NewManager(ctx context.Context, db *sql.DB) (*Manager, error) {
	manager := &Manager{db: db}

	origins, err := manager.readOrigins(ctx)
	if err != nil {
		return nil, fmt.Errorf("load CORS origins: %w", err)
	}
	manager.store(origins)

	return manager, nil
}

func (m *Manager) List(ctx context.Context, userID int64) ([]string, error) {
	owner, err := isOwner(ctx, m.db, userID)
	if err != nil {
		return nil, fmt.Errorf("check CORS settings access: %w", err)
	}
	if !owner {
		return nil, ErrForbidden
	}

	return m.ConfiguredOrigins(), nil
}

func (m *Manager) Replace(
	ctx context.Context,
	userID int64,
	origins []string,
) ([]string, error) {
	normalized, err := normalizeOrigins(origins)
	if err != nil {
		return nil, err
	}

	// Serialize the database commit and cache publication so two concurrent
	// updates cannot leave an older snapshot active after a newer commit.
	m.updates.Lock()
	defer m.updates.Unlock()

	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin CORS origins transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	owner, err := isOwner(ctx, tx, userID)
	if err != nil {
		return nil, fmt.Errorf("check CORS settings access: %w", err)
	}
	if !owner {
		return nil, ErrForbidden
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM cors_origins"); err != nil {
		return nil, fmt.Errorf("clear CORS origins: %w", err)
	}

	const insert = `
	INSERT INTO cors_origins (origin, created_by)
	VALUES (?, ?)
	`
	for _, origin := range normalized {
		if _, err := tx.ExecContext(ctx, insert, origin, userID); err != nil {
			return nil, fmt.Errorf("insert CORS origin: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit CORS origins: %w", err)
	}

	m.store(normalized)
	return append([]string(nil), normalized...), nil
}

func (m *Manager) ConfiguredOrigins() []string {
	snapshot := m.origins.Load()
	if snapshot == nil {
		return []string{}
	}

	return append([]string(nil), snapshot.list...)
}

// OriginPatterns returns exact patterns suitable for coder/websocket. The
// packaged Wails origins are always included and are not persisted settings.
func (m *Manager) OriginPatterns() []string {
	configured := m.ConfiguredOrigins()
	patterns := make([]string, 0, len(builtInOrigins)+len(configured))
	for _, origin := range builtInOrigins {
		patterns = append(patterns, exactMatchPattern(origin))
	}
	for _, origin := range configured {
		patterns = append(patterns, exactMatchPattern(origin))
	}
	return patterns
}

func (m *Manager) originAllowed(origin string) bool {
	for _, builtIn := range builtInOrigins {
		if origin == builtIn {
			return true
		}
	}

	snapshot := m.origins.Load()
	if snapshot == nil {
		return false
	}
	_, ok := snapshot.set[origin]
	return ok
}

func (m *Manager) readOrigins(ctx context.Context) ([]string, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT origin
		FROM cors_origins
		ORDER BY origin
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	origins := make([]string, 0)
	for rows.Next() {
		var origin string
		if err := rows.Scan(&origin); err != nil {
			return nil, err
		}
		origins = append(origins, origin)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return normalizeOrigins(origins)
}

func (m *Manager) store(origins []string) {
	list := append([]string(nil), origins...)
	set := make(map[string]struct{}, len(list))
	for _, origin := range list {
		set[origin] = struct{}{}
	}
	m.origins.Store(&originSnapshot{list: list, set: set})
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func isOwner(ctx context.Context, queryer rowQuerier, userID int64) (bool, error) {
	if userID <= 0 {
		return false, nil
	}

	const query = `
	SELECT EXISTS (
		SELECT 1
		FROM server_members
		WHERE user_id = ?
		  AND role = 'owner'
	)
	`

	var owner bool
	if err := queryer.QueryRowContext(ctx, query, userID).Scan(&owner); err != nil {
		return false, err
	}
	return owner, nil
}

func normalizeOrigins(origins []string) ([]string, error) {
	if len(origins) > maxOrigins {
		return nil, ErrTooManyOrigins
	}

	unique := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		normalized, err := normalizeOrigin(origin)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %v", ErrInvalidOrigin, origin, err)
		}
		unique[normalized] = struct{}{}
	}

	result := make([]string, 0, len(unique))
	for origin := range unique {
		result = append(result, origin)
	}
	sort.Strings(result)
	return result, nil
}

func normalizeOrigin(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxOriginBytes {
		return "", errors.New("origin is empty or too long")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", errors.New("origin is not a valid URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return "", errors.New("origin must contain only a host and optional port")
	}
	if (parsed.Path != "" && parsed.Path != "/") ||
		parsed.RawPath != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("paths, queries and fragments are not allowed")
	}

	hostname := strings.ToLower(parsed.Hostname())
	if !validHostname(hostname) {
		return "", errors.New("host must be an IP address or a valid ASCII hostname")
	}
	port := parsed.Port()
	if port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("port must be between 1 and 65535")
		}
	}
	if (parsed.Scheme == "http" && port == "80") ||
		(parsed.Scheme == "https" && port == "443") {
		port = ""
	}

	host := hostname
	if port != "" {
		host = net.JoinHostPort(hostname, port)
	} else if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}

	return (&url.URL{Scheme: parsed.Scheme, Host: host}).String(), nil
}

func validHostname(hostname string) bool {
	if net.ParseIP(hostname) != nil {
		return true
	}
	if len(hostname) == 0 || len(hostname) > 253 {
		return false
	}

	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character < 'a' || character > 'z') &&
				(character < '0' || character > '9') &&
				character != '-' {
				return false
			}
		}
	}
	return true
}

// coder/websocket uses path.Match for origin patterns. Escaping its
// metacharacters keeps every configured origin an exact match, including IPv6
// addresses whose URL form contains square brackets.
func exactMatchPattern(origin string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`*`, `\*`,
		`?`, `\?`,
		`[`, `\[`,
	)
	return replacer.Replace(origin)
}
