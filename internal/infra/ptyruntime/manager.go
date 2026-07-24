package ptyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"
	terminaldomain "github.com/nzlov/anycode/internal/domain/terminal"
)

const (
	defaultCols        = 120
	defaultRows        = 32
	defaultReplayBytes = 2 << 20
	defaultStopTimeout = 3 * time.Second
	subscriberQueue    = 128
	commandHistorySize = 100
)

type Option func(*Manager)

func WithShell(shell string) Option {
	return func(manager *Manager) {
		manager.shell = strings.TrimSpace(shell)
	}
}

func WithReplayBytes(size int) Option {
	return func(manager *Manager) {
		if size > 0 {
			manager.replayBytes = size
		}
	}
}

func WithStopTimeout(timeout time.Duration) Option {
	return func(manager *Manager) {
		if timeout > 0 {
			manager.stopTimeout = timeout
		}
	}
}

func WithHistoryDir(path string) Option {
	return func(manager *Manager) {
		manager.historyDir = strings.TrimSpace(path)
	}
}

type Manager struct {
	mu           sync.Mutex
	runs         map[terminaldomain.SessionID]*run
	shell        string
	replayBytes  int
	stopTimeout  time.Duration
	historyDir   string
	nextRunID    atomic.Uint64
	nextClientID atomic.Uint64
}

type run struct {
	mu          sync.Mutex
	persistMu   sync.Mutex
	id          terminaldomain.RunID
	sessionID   terminaldomain.SessionID
	cmd         *exec.Cmd
	ptmx        *os.File
	replay      []byte
	subscribers map[uint64]chan []byte
	exit        chan terminaldomain.ExitResult
	done        chan struct{}
	active      bool
	workdir     string
	pendingDir  string
	input       []byte
	commands    []string
	escapeInput bool
	historyFile *os.File
	historyPath string
	statePath   string
	historySize int64
}

type historyState struct {
	Workdir    string   `json:"workdir"`
	PendingDir string   `json:"pendingDir,omitempty"`
	Commands   []string `json:"commands,omitempty"`
}

func New(options ...Option) *Manager {
	manager := &Manager{
		runs:        make(map[terminaldomain.SessionID]*run),
		shell:       defaultShell(),
		replayBytes: defaultReplayBytes,
		stopTimeout: defaultStopTimeout,
	}
	for _, option := range options {
		option(manager)
	}
	return manager
}

func (m *Manager) Start(_ context.Context, input terminaldomain.StartInput) (terminaldomain.Handle, error) {
	if m == nil {
		return terminaldomain.Handle{}, errors.New("terminal runtime is nil")
	}
	if input.SessionID == "" {
		return terminaldomain.Handle{}, errors.New("terminal session id is required")
	}
	history, replay, historyPath, statePath, historySize, err := m.loadHistory(input.SessionID)
	if err != nil {
		return terminaldomain.Handle{}, err
	}
	configuredWorkdir := restoredWorkdir(history)
	if strings.TrimSpace(configuredWorkdir) == "" {
		configuredWorkdir = input.Workdir
	}
	workdir, err := terminalWorkdir(configuredWorkdir)
	if err != nil {
		return terminaldomain.Handle{}, err
	}
	if !filepath.IsAbs(workdir) {
		return terminaldomain.Handle{}, errors.New("terminal workdir must be an absolute path")
	}
	cols, rows := normalizeSize(input.Cols, input.Rows)

	m.mu.Lock()
	if current := m.runs[input.SessionID]; current != nil && current.isActive() {
		m.mu.Unlock()
		return terminaldomain.Handle{}, terminaldomain.ErrRunActive
	}
	runID := terminaldomain.RunID(fmt.Sprintf("terminal-%d", m.nextRunID.Add(1)))
	cmd := exec.Command(m.shell)
	cmd.Dir = workdir
	cmd.Env = terminalEnvironment(os.Environ())
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		m.mu.Unlock()
		return terminaldomain.Handle{}, fmt.Errorf("start terminal shell: %w", err)
	}
	var historyFile *os.File
	if historyPath != "" {
		historyFile, err = os.OpenFile(historyPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			_ = ptmx.Close()
			_ = cmd.Process.Kill()
			m.mu.Unlock()
			return terminaldomain.Handle{}, fmt.Errorf("open terminal history: %w", err)
		}
	}
	current := &run{
		id:          runID,
		sessionID:   input.SessionID,
		cmd:         cmd,
		ptmx:        ptmx,
		subscribers: make(map[uint64]chan []byte),
		exit:        make(chan terminaldomain.ExitResult, 1),
		done:        make(chan struct{}),
		active:      true,
		workdir:     workdir,
		replay:      replay,
		commands:    append([]string(nil), history.Commands...),
		historyFile: historyFile,
		historyPath: historyPath,
		statePath:   statePath,
		historySize: historySize,
	}
	m.runs[input.SessionID] = current
	m.mu.Unlock()
	m.persistState(current)

	readDone := make(chan struct{})
	go m.readOutput(current, readDone)
	go m.wait(current, readDone)
	return terminaldomain.Handle{RunID: runID, Exit: current.exit}, nil
}

func (m *Manager) Write(sessionID terminaldomain.SessionID, data []byte) error {
	current, err := m.activeRun(sessionID)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := current.ptmx.Write(data); err != nil {
		return fmt.Errorf("write terminal input: %w", err)
	}
	if current.recordInput(data) {
		m.persistState(current)
	}
	return nil
}

func (m *Manager) Resize(sessionID terminaldomain.SessionID, cols uint16, rows uint16) error {
	current, err := m.activeRun(sessionID)
	if err != nil {
		return err
	}
	cols, rows = normalizeSize(cols, rows)
	if err := pty.Setsize(current.ptmx, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return fmt.Errorf("resize terminal: %w", err)
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, sessionID terminaldomain.SessionID) error {
	current, err := m.activeRun(sessionID)
	if errors.Is(err, terminaldomain.ErrRunNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if current.cmd.Process != nil {
		_ = signalProcessGroup(current.cmd.Process.Pid, syscall.SIGTERM)
	}
	wait := m.stopTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
	}
	if wait < 0 {
		wait = 0
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-current.done:
		return nil
	case <-ctx.Done():
	case <-timer.C:
	}
	if current.cmd.Process != nil {
		_ = signalProcessGroup(current.cmd.Process.Pid, syscall.SIGKILL)
	}
	select {
	case <-current.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop terminal: %w", ctx.Err())
	}
}

func (m *Manager) Subscribe(sessionID terminaldomain.SessionID) (terminaldomain.OutputSubscription, error) {
	if m == nil {
		return terminaldomain.OutputSubscription{}, terminaldomain.ErrRunNotFound
	}
	current, err := m.runOrHistory(sessionID)
	if err != nil {
		return terminaldomain.OutputSubscription{}, err
	}
	clientID := m.nextClientID.Add(1)
	output := make(chan []byte, subscriberQueue)
	current.mu.Lock()
	replay := append([]byte(nil), current.replay...)
	if current.active {
		current.subscribers[clientID] = output
	} else {
		close(output)
	}
	current.mu.Unlock()
	var once sync.Once
	return terminaldomain.OutputSubscription{
		Replay: replay,
		Output: output,
		Close: func() {
			once.Do(func() {
				current.mu.Lock()
				if channel, ok := current.subscribers[clientID]; ok {
					delete(current.subscribers, clientID)
					close(channel)
				}
				current.mu.Unlock()
			})
		},
	}, nil
}

func (m *Manager) Summary(sessionID terminaldomain.SessionID) (terminaldomain.Summary, error) {
	if m == nil {
		return terminaldomain.Summary{}, terminaldomain.ErrRunNotFound
	}
	current, err := m.runOrHistory(sessionID)
	if err != nil {
		return terminaldomain.Summary{}, err
	}
	changed := false
	current.mu.Lock()
	if current.pendingDir != "" {
		if resolved, err := filepath.EvalSymlinks(current.pendingDir); err == nil {
			if info, statErr := os.Stat(resolved); statErr == nil && info.IsDir() {
				current.workdir = resolved
				current.pendingDir = ""
				changed = true
			}
		}
	}
	workdir := current.workdir
	commands := append([]string(nil), current.commands...)
	current.mu.Unlock()
	if changed {
		m.persistState(current)
	}
	return terminaldomain.Summary{
		CurrentDirectory: displayWorkdir(workdir),
		Commands:         commands,
	}, nil
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	runs := make([]*run, 0, len(m.runs))
	for _, current := range m.runs {
		if current.isActive() {
			runs = append(runs, current)
		}
	}
	m.mu.Unlock()
	var errs []error
	for _, current := range runs {
		ctx, cancel := context.WithTimeout(context.Background(), m.stopTimeout)
		err := m.Stop(ctx, current.sessionID)
		cancel()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *Manager) activeRun(sessionID terminaldomain.SessionID) (*run, error) {
	if m == nil {
		return nil, terminaldomain.ErrRunNotFound
	}
	m.mu.Lock()
	current := m.runs[sessionID]
	m.mu.Unlock()
	if current == nil || !current.isActive() {
		return nil, terminaldomain.ErrRunNotFound
	}
	return current, nil
}

func (m *Manager) readOutput(current *run, done chan<- struct{}) {
	defer close(done)
	buffer := make([]byte, 32<<10)
	for {
		count, err := current.ptmx.Read(buffer)
		if count > 0 {
			m.broadcast(current, buffer[:count])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				// Linux returns EIO when the slave side of a PTY exits.
				var pathErr *os.PathError
				if !errors.As(err, &pathErr) || !errors.Is(pathErr.Err, syscall.EIO) {
					m.broadcast(current, []byte("\r\n[terminal output closed]\r\n"))
				}
			}
			return
		}
	}
}

func (m *Manager) broadcast(current *run, data []byte) {
	chunk := append([]byte(nil), data...)
	current.mu.Lock()
	current.replay = append(current.replay, chunk...)
	if overflow := len(current.replay) - m.replayBytes; overflow > 0 {
		current.replay = append([]byte(nil), current.replay[overflow:]...)
	}
	if current.historyFile != nil {
		if count, err := current.historyFile.Write(chunk); err == nil {
			current.historySize += int64(count)
		} else {
			_ = current.historyFile.Close()
			current.historyFile = nil
		}
		if current.historySize > int64(m.replayBytes*2) {
			m.compactHistoryLocked(current)
		}
	}
	for id, subscriber := range current.subscribers {
		select {
		case subscriber <- chunk:
		default:
			close(subscriber)
			delete(current.subscribers, id)
		}
	}
	current.mu.Unlock()
}

func (m *Manager) wait(current *run, readDone <-chan struct{}) {
	err := current.cmd.Wait()
	_ = current.ptmx.Close()
	<-readDone
	current.mu.Lock()
	current.active = false
	if current.historyFile != nil {
		_ = current.historyFile.Close()
		current.historyFile = nil
	}
	for id, subscriber := range current.subscribers {
		close(subscriber)
		delete(current.subscribers, id)
	}
	current.mu.Unlock()
	result := terminaldomain.ExitResult{RunID: current.id}
	if current.cmd.ProcessState != nil {
		code := current.cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			result.Err = err
		}
	}
	current.exit <- result
	close(current.exit)
	close(current.done)
}

func (r *run) isActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

func (r *run) recordInput(data []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := false
	for _, value := range data {
		if r.escapeInput {
			if value == '[' {
				continue
			}
			if value >= 0x40 && value <= 0x7e {
				r.escapeInput = false
			}
			continue
		}
		switch value {
		case 0x1b:
			r.escapeInput = true
		case '\r', '\n':
			command := strings.TrimSpace(string(r.input))
			r.input = r.input[:0]
			if command == "" {
				continue
			}
			r.commands = append(r.commands, command)
			changed = true
			if overflow := len(r.commands) - commandHistorySize; overflow > 0 {
				r.commands = append([]string(nil), r.commands[overflow:]...)
			}
			r.trackDirectory(command)
		case 0x7f, 0x08:
			if len(r.input) > 0 {
				r.input = r.input[:len(r.input)-1]
			}
		case 0x15:
			r.input = r.input[:0]
		default:
			if value >= 0x20 {
				r.input = append(r.input, value)
			}
		}
	}
	return changed
}

func (m *Manager) runOrHistory(sessionID terminaldomain.SessionID) (*run, error) {
	m.mu.Lock()
	current := m.runs[sessionID]
	m.mu.Unlock()
	if current != nil {
		return current, nil
	}
	state, replay, historyPath, statePath, historySize, err := m.loadHistory(sessionID)
	if err != nil {
		return nil, err
	}
	if len(replay) == 0 && state.Workdir == "" && state.PendingDir == "" && len(state.Commands) == 0 {
		return nil, terminaldomain.ErrRunNotFound
	}
	workdir := restoredWorkdir(state)
	if workdir == "" {
		workdir, _ = terminalWorkdir("")
	}
	restored := &run{
		sessionID:   sessionID,
		replay:      replay,
		subscribers: make(map[uint64]chan []byte),
		workdir:     workdir,
		pendingDir:  state.PendingDir,
		commands:    append([]string(nil), state.Commands...),
		historyPath: historyPath,
		statePath:   statePath,
		historySize: historySize,
	}
	m.mu.Lock()
	if current = m.runs[sessionID]; current == nil {
		m.runs[sessionID] = restored
		current = restored
	}
	m.mu.Unlock()
	return current, nil
}

func (m *Manager) loadHistory(sessionID terminaldomain.SessionID) (historyState, []byte, string, string, int64, error) {
	if m.historyDir == "" {
		return historyState{}, nil, "", "", 0, nil
	}
	if err := os.MkdirAll(m.historyDir, 0o700); err != nil {
		return historyState{}, nil, "", "", 0, fmt.Errorf("create terminal history directory: %w", err)
	}
	name := fmt.Sprintf("%x", sha256.Sum256([]byte(sessionID)))
	historyPath := filepath.Join(m.historyDir, name+".log")
	statePath := filepath.Join(m.historyDir, name+".json")
	state := historyState{}
	stateData, stateErr := os.ReadFile(statePath)
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return historyState{}, nil, "", "", 0, fmt.Errorf("read terminal state: %w", stateErr)
	}
	if len(stateData) > 0 && json.Unmarshal(stateData, &state) != nil {
		state = historyState{}
	}
	replay, size, err := readHistoryTail(historyPath, m.replayBytes)
	if err != nil {
		return historyState{}, nil, "", "", 0, err
	}
	return state, replay, historyPath, statePath, size, nil
}

func readHistoryTail(path string, limit int) ([]byte, int64, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open terminal history: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, fmt.Errorf("stat terminal history: %w", err)
	}
	start := info.Size() - int64(limit)
	if start < 0 {
		start = 0
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek terminal history: %w", err)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, 0, fmt.Errorf("read terminal history: %w", err)
	}
	return data, info.Size(), nil
}

func restoredWorkdir(state historyState) string {
	for _, candidate := range []string{state.PendingDir, state.Workdir} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(resolved); err == nil && info.IsDir() {
			return resolved
		}
	}
	return ""
}

func (m *Manager) persistState(current *run) {
	current.mu.Lock()
	statePath := current.statePath
	state := historyState{
		Workdir:    current.workdir,
		PendingDir: current.pendingDir,
		Commands:   append([]string(nil), current.commands...),
	}
	current.mu.Unlock()
	if statePath == "" {
		return
	}
	current.persistMu.Lock()
	defer current.persistMu.Unlock()
	data, err := json.Marshal(state)
	if err == nil {
		_ = os.WriteFile(statePath, data, 0o600)
	}
}

func (m *Manager) compactHistoryLocked(current *run) {
	if current.historyPath == "" {
		return
	}
	if current.historyFile != nil {
		_ = current.historyFile.Close()
	}
	if err := os.WriteFile(current.historyPath, current.replay, 0o600); err != nil {
		current.historyFile = nil
		return
	}
	current.historySize = int64(len(current.replay))
	current.historyFile, _ = os.OpenFile(current.historyPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

func (r *run) trackDirectory(command string) {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "cd" || len(fields) > 2 {
		return
	}
	target := ""
	if len(fields) == 2 {
		target = strings.Trim(fields[1], "\"'")
	}
	if target == "-" {
		return
	}
	home, _ := os.UserHomeDir()
	switch {
	case target == "", target == "~", target == "$HOME", target == "${HOME}":
		target = home
	case strings.HasPrefix(target, "~/") && home != "":
		target = filepath.Join(home, strings.TrimPrefix(target, "~/"))
	case !filepath.IsAbs(target):
		base := r.workdir
		if r.pendingDir != "" {
			base = r.pendingDir
		}
		target = filepath.Join(base, target)
	}
	r.pendingDir = filepath.Clean(target)
}

func terminalWorkdir(configured string) (string, error) {
	workdir := strings.TrimSpace(configured)
	if workdir != "" {
		return workdir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errors.New("terminal home directory is unavailable")
	}
	return home, nil
}

func displayWorkdir(workdir string) string {
	cleaned := filepath.Clean(workdir)
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		home = filepath.Clean(home)
		if cleaned == home {
			return "~"
		}
		if relative, relativeErr := filepath.Rel(home, cleaned); relativeErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Join("~", relative)
		}
	}
	base := filepath.Base(cleaned)
	if base == "." || base == string(filepath.Separator) {
		return string(filepath.Separator)
	}
	return filepath.Join("…", base)
}

func normalizeSize(cols uint16, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = defaultCols
	}
	if rows == 0 {
		rows = defaultRows
	}
	if cols > 500 {
		cols = 500
	}
	if rows > 300 {
		rows = 300
	}
	return cols, rows
}

func defaultShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		return shell
	}
	return "/bin/sh"
}

func terminalEnvironment(environment []string) []string {
	result := make([]string, 0, len(environment)+2)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "TERM=") || strings.HasPrefix(entry, "COLORTERM=") {
			continue
		}
		result = append(result, entry)
	}
	return append(result, "TERM=xterm-256color", "COLORTERM=truecolor")
}

func signalProcessGroup(pid int, signal syscall.Signal) error {
	if pid <= 0 {
		return nil
	}
	if err := syscall.Kill(-pid, signal); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
