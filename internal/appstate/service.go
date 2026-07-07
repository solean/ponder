package appstate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cschnabel/mtgdata/internal/db"
	"github.com/cschnabel/mtgdata/internal/ingest"
	"github.com/cschnabel/mtgdata/internal/model"
	"github.com/cschnabel/mtgdata/internal/version"
)

const defaultPollInterval = 2 * time.Second

type Options struct {
	Store               *db.Store
	DBPath              string
	SupportDir          string
	ConfigPath          string
	DefaultLogPath      string
	DefaultPrevLogPath  string
	DefaultPollInterval time.Duration
	Capabilities        Capabilities
}

// Capabilities advertises native-shell integrations available to the frontend.
// Both are false in headless serve mode; the desktop app enables them.
type Capabilities struct {
	PickFile bool `json:"pickFile"`
	Reveal   bool `json:"reveal"`
}

type Config struct {
	LogPath             string `json:"logPath"`
	PollIntervalSeconds int    `json:"pollIntervalSeconds"`
	IncludePrev         bool   `json:"includePrev"`
}

type OperationResult struct {
	Kind            string   `json:"kind"`
	Files           []string `json:"files"`
	LinesRead       int64    `json:"linesRead"`
	BytesRead       int64    `json:"bytesRead"`
	RawEventsStored int64    `json:"rawEventsStored"`
	MatchesUpserted int64    `json:"matchesUpserted"`
	RankSnapshots   int64    `json:"rankSnapshots"`
	DecksUpserted   int64    `json:"decksUpserted"`
	DraftPicksAdded int64    `json:"draftPicksAdded"`
	StartedAt       string   `json:"startedAt"`
	CompletedAt     string   `json:"completedAt"`
	DurationMs      int64    `json:"durationMs"`
	HasActivity     bool     `json:"hasActivity"`
}

type Status struct {
	Version               string           `json:"version"`
	DBPath                string           `json:"dbPath"`
	DBSizeBytes           int64            `json:"dbSizeBytes"`
	SupportDir            string           `json:"supportDir"`
	ConfigPath            string           `json:"configPath"`
	DefaultLogPath        string           `json:"defaultLogPath"`
	DefaultPrevLogPath    string           `json:"defaultPrevLogPath"`
	Config                Config           `json:"config"`
	ActiveLogPath         string           `json:"activeLogPath"`
	PreviousLogPath       string           `json:"previousLogPath"`
	ActiveLogPathExists   bool             `json:"activeLogPathExists"`
	PreviousLogPathExists bool             `json:"previousLogPathExists"`
	LiveRunning           bool             `json:"liveRunning"`
	LiveStartedAt         string           `json:"liveStartedAt,omitempty"`
	LiveLastTickAt        string           `json:"liveLastTickAt,omitempty"`
	LastImport            *OperationResult `json:"lastImport,omitempty"`
	LastLiveActivity      *OperationResult `json:"lastLiveActivity,omitempty"`
	LastError             string           `json:"lastError,omitempty"`
	Capabilities          Capabilities     `json:"capabilities"`
}

type Service struct {
	store              *db.Store
	dbPath             string
	supportDir         string
	configPath         string
	defaultLogPath     string
	defaultPrevLogPath string
	defaultPoll        time.Duration
	capabilities       Capabilities

	mu               sync.RWMutex
	config           Config
	liveRunning      bool
	liveStartedAt    time.Time
	liveLastTickAt   time.Time
	liveCancel       context.CancelFunc
	liveDone         chan struct{}
	lastImport       *OperationResult
	lastLiveActivity *OperationResult
	lastError        string
}

func NewService(opts Options) (*Service, error) {
	if opts.Store == nil {
		return nil, fmt.Errorf("appstate store is required")
	}
	dbPath := strings.TrimSpace(opts.DBPath)
	if dbPath == "" {
		return nil, fmt.Errorf("appstate db path is required")
	}

	supportDir, err := resolveSupportDir(opts.SupportDir)
	if err != nil {
		return nil, err
	}

	configPath := strings.TrimSpace(opts.ConfigPath)
	if configPath == "" {
		configPath = filepath.Join(supportDir, "config.json")
	}

	currentLogPath := strings.TrimSpace(opts.DefaultLogPath)
	prevLogPath := strings.TrimSpace(opts.DefaultPrevLogPath)
	if currentLogPath == "" || prevLogPath == "" {
		current, prev, err := DefaultMTGALogPaths()
		if err == nil {
			if currentLogPath == "" {
				currentLogPath = current
			}
			if prevLogPath == "" {
				prevLogPath = prev
			}
		}
	}

	poll := opts.DefaultPollInterval
	if poll <= 0 {
		poll = defaultPollInterval
	}

	cfg := Config{
		PollIntervalSeconds: max(1, int(poll.Round(time.Second)/time.Second)),
		IncludePrev:         true,
	}

	if raw, err := os.ReadFile(configPath); err == nil {
		var saved Config
		if unmarshalErr := json.Unmarshal(raw, &saved); unmarshalErr == nil {
			cfg = normalizeConfig(saved, poll)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read appstate config: %w", err)
	}

	return &Service{
		store:              opts.Store,
		dbPath:             dbPath,
		supportDir:         supportDir,
		configPath:         configPath,
		defaultLogPath:     currentLogPath,
		defaultPrevLogPath: prevLogPath,
		defaultPoll:        poll,
		capabilities:       opts.Capabilities,
		config:             normalizeConfig(cfg, poll),
	}, nil
}

func (s *Service) Status() Status {
	s.mu.RLock()
	cfg := s.config
	liveRunning := s.liveRunning
	liveStartedAt := s.liveStartedAt
	liveLastTickAt := s.liveLastTickAt
	lastImport := cloneOperationResult(s.lastImport)
	lastLiveActivity := cloneOperationResult(s.lastLiveActivity)
	lastError := s.lastError
	s.mu.RUnlock()

	activeLogPath := strings.TrimSpace(cfg.LogPath)
	if activeLogPath == "" {
		activeLogPath = s.defaultLogPath
	}

	prevLogPath := ""
	if strings.TrimSpace(cfg.LogPath) == "" && cfg.IncludePrev {
		prevLogPath = s.defaultPrevLogPath
	}

	return Status{
		Version:               version.Version,
		DBPath:                s.dbPath,
		DBSizeBytes:           databaseSize(s.dbPath),
		SupportDir:            s.supportDir,
		ConfigPath:            s.configPath,
		DefaultLogPath:        s.defaultLogPath,
		DefaultPrevLogPath:    s.defaultPrevLogPath,
		Config:                cfg,
		ActiveLogPath:         activeLogPath,
		PreviousLogPath:       prevLogPath,
		ActiveLogPathExists:   fileExists(activeLogPath),
		PreviousLogPathExists: fileExists(prevLogPath),
		LiveRunning:           liveRunning,
		LiveStartedAt:         formatTime(liveStartedAt),
		LiveLastTickAt:        formatTime(liveLastTickAt),
		LastImport:            lastImport,
		LastLiveActivity:      lastLiveActivity,
		LastError:             lastError,
		Capabilities:          s.capabilities,
	}
}

// RevealablePaths lists the filesystem paths the UI displays; the desktop
// reveal endpoint refuses anything outside this set.
func (s Status) RevealablePaths() []string {
	return []string{
		s.DBPath,
		s.SupportDir,
		s.ConfigPath,
		s.ActiveLogPath,
		s.PreviousLogPath,
		s.DefaultLogPath,
		s.DefaultPrevLogPath,
	}
}

func (s *Service) UpdateConfig(next Config) (Status, error) {
	cfg := normalizeConfig(next, s.defaultPoll)
	if err := s.saveConfig(cfg); err != nil {
		return s.Status(), err
	}

	s.mu.Lock()
	wasRunning := s.liveRunning
	s.config = cfg
	s.mu.Unlock()

	if wasRunning {
		if _, err := s.StopLive(); err != nil {
			return s.Status(), err
		}
		if _, err := s.StartLive(); err != nil {
			return s.Status(), err
		}
	}

	return s.Status(), nil
}

func (s *Service) ParseNow(ctx context.Context, resume bool) (OperationResult, error) {
	s.mu.RLock()
	cfg := s.config
	liveRunning := s.liveRunning
	s.mu.RUnlock()

	if liveRunning {
		return OperationResult{}, fmt.Errorf("stop live tracking before running a manual import")
	}

	logPaths, err := ResolveParseLogPaths(cfg.LogPath, cfg.IncludePrev)
	if err != nil {
		s.setLastError(err.Error())
		return OperationResult{}, err
	}

	parser := ingest.NewParser(s.store)
	statsByFile := make([]model.ParseStats, 0, len(logPaths))
	for _, logPath := range logPaths {
		stats, err := parser.ParseFile(ctx, logPath, resume)
		if err != nil {
			s.setLastError(err.Error())
			return OperationResult{}, fmt.Errorf("parse %s: %w", logPath, err)
		}
		statsByFile = append(statsByFile, stats)
	}

	result := summarizeOperation("import", logPaths, statsByFile)

	s.mu.Lock()
	s.lastImport = cloneOperationResult(&result)
	s.lastError = ""
	s.mu.Unlock()

	return result, nil
}

func (s *Service) StartLive() (Status, error) {
	s.mu.RLock()
	if s.liveRunning {
		s.mu.RUnlock()
		return s.Status(), nil
	}
	cfg := s.config
	s.mu.RUnlock()

	activeLogPath := strings.TrimSpace(cfg.LogPath)
	if activeLogPath == "" {
		activeLogPath = s.defaultLogPath
	}
	if activeLogPath == "" {
		err := fmt.Errorf("no active MTGA log path configured")
		s.setLastError(err.Error())
		return s.Status(), err
	}
	if !fileExists(activeLogPath) {
		err := fmt.Errorf("active log path not found: %s", activeLogPath)
		s.setLastError(err.Error())
		return s.Status(), err
	}

	poll := time.Duration(cfg.PollIntervalSeconds) * time.Second
	if poll <= 0 {
		poll = s.defaultPoll
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	parser := ingest.NewParser(s.store)
	startedAt := time.Now().UTC()

	s.mu.Lock()
	if s.liveRunning {
		s.mu.Unlock()
		cancel()
		return s.Status(), nil
	}
	s.liveRunning = true
	s.liveStartedAt = startedAt
	s.liveLastTickAt = time.Time{}
	s.liveCancel = cancel
	s.liveDone = done
	s.lastError = ""
	s.mu.Unlock()

	go s.runLiveLoop(ctx, done, parser, activeLogPath, poll)

	return s.Status(), nil
}

func (s *Service) StopLive() (Status, error) {
	s.mu.Lock()
	if !s.liveRunning {
		s.mu.Unlock()
		return s.Status(), nil
	}
	cancel := s.liveCancel
	done := s.liveDone
	s.liveRunning = false
	s.liveCancel = nil
	s.liveDone = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	return s.Status(), nil
}

func (s *Service) runLiveLoop(
	ctx context.Context,
	done chan struct{},
	parser *ingest.Parser,
	activeLogPath string,
	poll time.Duration,
) {
	defer close(done)

	runTick := func() bool {
		stats, err := parser.ParseFile(ctx, activeLogPath, true)
		now := time.Now().UTC()

		s.mu.Lock()
		s.liveLastTickAt = now
		if err != nil {
			s.lastError = fmt.Sprintf("live parse error: %v", err)
			s.mu.Unlock()
			return false
		}

		result := summarizeOperation("live", []string{activeLogPath}, []model.ParseStats{stats})
		result.HasActivity = hasActivity(stats)
		if result.HasActivity {
			s.lastLiveActivity = cloneOperationResult(&result)
			s.lastError = ""
		}
		s.mu.Unlock()
		return true
	}

	runTick()

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runTick()
		}
	}
}

func (s *Service) saveConfig(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	payload, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(s.configPath, append(payload, '\n'), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (s *Service) setLastError(message string) {
	s.mu.Lock()
	s.lastError = strings.TrimSpace(message)
	s.mu.Unlock()
}

func resolveSupportDir(explicit string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		return explicit, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, "mtgdata"), nil
}

func normalizeConfig(cfg Config, poll time.Duration) Config {
	cfg.LogPath = strings.TrimSpace(cfg.LogPath)
	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = max(1, int(poll.Round(time.Second)/time.Second))
	}
	return cfg
}

func summarizeOperation(kind string, paths []string, stats []model.ParseStats) OperationResult {
	out := OperationResult{
		Kind:  kind,
		Files: append([]string(nil), paths...),
	}
	if len(stats) == 0 {
		now := time.Now().UTC()
		out.StartedAt = now.Format(time.RFC3339Nano)
		out.CompletedAt = out.StartedAt
		return out
	}

	startedAt := stats[0].StartedAt
	completedAt := stats[0].CompletedAt
	for _, item := range stats {
		if item.StartedAt.Before(startedAt) {
			startedAt = item.StartedAt
		}
		if item.CompletedAt.After(completedAt) {
			completedAt = item.CompletedAt
		}
		out.LinesRead += item.LinesRead
		out.BytesRead += item.BytesRead
		out.RawEventsStored += item.RawEventsStored
		out.MatchesUpserted += item.MatchesUpserted
		out.RankSnapshots += item.RankSnapshots
		out.DecksUpserted += item.DecksUpserted
		out.DraftPicksAdded += item.DraftPicksAdded
	}
	out.StartedAt = formatTime(startedAt)
	out.CompletedAt = formatTime(completedAt)
	out.DurationMs = completedAt.Sub(startedAt).Milliseconds()
	out.HasActivity = out.LinesRead > 0 ||
		out.RawEventsStored > 0 ||
		out.MatchesUpserted > 0 ||
		out.DecksUpserted > 0 ||
		out.DraftPicksAdded > 0
	return out
}

func cloneOperationResult(result *OperationResult) *OperationResult {
	if result == nil {
		return nil
	}
	cloned := *result
	cloned.Files = append([]string(nil), result.Files...)
	return &cloned
}

// databaseSize totals the SQLite database file plus its -wal/-shm sidecars.
func databaseSize(dbPath string) int64 {
	dbPath = strings.TrimSpace(dbPath)
	if dbPath == "" {
		return 0
	}
	var total int64
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			total += info.Size()
		}
	}
	return total
}

func fileExists(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func hasActivity(stats model.ParseStats) bool {
	return stats.LinesRead > 0 ||
		stats.RawEventsStored > 0 ||
		stats.MatchesUpserted > 0 ||
		stats.DecksUpserted > 0 ||
		stats.DraftPicksAdded > 0
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
