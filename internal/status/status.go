// Package status exposes a read-only snapshot of each running watcher to the same user.
package status

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"rexpresence/internal/presence"
)

// Live owns a Unix socket in a private directory. Status is never persisted to disk.
type Live struct {
	listener net.Listener
	mu       sync.Mutex
	session  string
	mode     string
	discord  string
	snapshot Snapshot
}

// Snapshot is the latest effective activity and the watcher's startup logo.
type Snapshot struct {
	Activity *presence.Activity
	Mode     string
	Logo     string
}

func directory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".rexpresence"), nil
}

// Listen starts a local status endpoint for the current process.
func Listen() (*Live, error) {
	dir, err := directory()
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, fmt.Errorf("status directory %q must be a private directory", dir)
	}
	path := filepath.Join(dir, strconv.Itoa(os.Getpid())+".sock")
	// Only this process can own its PID; a previous instance may have crashed.
	if stale, err := os.Lstat(path); err == nil && stale.Mode()&os.ModeSocket != 0 {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	s := &Live{listener: listener, discord: "waiting", mode: "loading"}
	go s.serve()
	return s, nil
}

func (s *Live) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		go s.respond(conn)
	}
}

func (s *Live) respond(conn net.Conn) {
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	var request [1]byte
	n, _ := conn.Read(request[:])
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	s.mu.Lock()
	session, mode, discord, snapshot := s.session, s.mode, s.discord, s.snapshot
	s.mu.Unlock()
	if n == 1 && request[0] == '?' {
		snapshot.Mode = mode
		if snapshot.Activity != nil && (mode != "auto" && mode != "custom" || snapshot.Activity.Custom != (mode == "custom")) {
			snapshot.Activity = nil
		}
		json.NewEncoder(conn).Encode(snapshot)
		return
	}
	rex := "resolving session"
	if session != "" {
		rex = fmt.Sprintf("watching %q", session)
	}
	fmt.Fprintf(conn, "Watcher: running (PID %d)\nRex: %s\nMode: %s\nDiscord: %s\n", os.Getpid(), rex, mode, discord)
}

// Close stops serving status and unlinks the socket.
func (s *Live) Close() error { return s.listener.Close() }

// Mode records the applied mode without exposing custom text.
func (s *Live) Mode(mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode != mode {
		s.snapshot.Activity = nil
	}
	s.mode = mode
}

// Activity records an immutable copy of the effective activity and startup logo.
func (s *Live) Activity(p *presence.Activity, logo string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot.Logo = logo
	s.snapshot.Activity = nil
	if p != nil {
		copy := *p
		if p.Card != nil {
			card := *p.Card
			copy.Card = &card
		}
		s.snapshot.Activity = &copy
	}
}

// Session records the actual session resolved by Rex, not the user's selector.
func (s *Live) Session(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = name
}

// Discord records the latest state of the publisher.
func (s *Live) Discord(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.discord = state
}

// Print queries running instances; a stale socket never counts as a live process.
func Print(w io.Writer) error {
	dir, err := directory()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	found := false
	for _, entry := range entries {
		if entry.Type()&os.ModeSocket == 0 || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		conn, err := net.DialTimeout("unix", filepath.Join(dir, entry.Name()), time.Second)
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		conn.Write([]byte("s"))
		conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := io.Copy(w, io.LimitReader(conn, 8192))
		conn.Close()
		if err != nil {
			return err
		}
		found = found || n > 0
	}
	if !found {
		_, err = fmt.Fprintln(w, "Watcher: not running")
		return err
	}
	return nil
}

// PIDs returns the processes of running watchers, found by their live sockets.
func PIDs() ([]int, error) {
	dir, err := directory()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("status directory must be private")
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pids []int
	for _, entry := range entries {
		if entry.Type()&os.ModeSocket == 0 || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".sock"))
		if err != nil || pid <= 0 {
			continue
		}
		conn, err := net.DialTimeout("unix", filepath.Join(dir, entry.Name()), time.Second)
		if err != nil {
			continue
		}
		conn.Close()
		pids = append(pids, pid)
	}
	return pids, nil
}

// ReadSnapshot queries exactly one live watcher without persisting its activity.
func ReadSnapshot() (Snapshot, error) {
	var snapshot Snapshot
	dir, err := directory()
	if err != nil {
		return snapshot, errors.New("cannot locate watcher")
	}
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return snapshot, errors.New("watcher is not running")
	}
	if err != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return snapshot, errors.New("status directory must be private")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return snapshot, errors.New("cannot read status directory")
	}
	var selected net.Conn
	for _, entry := range entries {
		if entry.Type()&os.ModeSocket == 0 || filepath.Ext(entry.Name()) != ".sock" {
			continue
		}
		conn, err := net.DialTimeout("unix", filepath.Join(dir, entry.Name()), time.Second)
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return snapshot, errors.New("cannot connect to watcher")
		}
		if selected != nil {
			conn.Close()
			return snapshot, errors.New("multiple watchers are running")
		}
		selected = conn
		defer selected.Close()
		selected.SetDeadline(time.Now().Add(time.Second))
		if _, err := selected.Write([]byte("?")); err != nil {
			return snapshot, errors.New("cannot request watcher snapshot")
		}
	}
	if selected == nil {
		return snapshot, errors.New("watcher is not running")
	}
	if err := json.NewDecoder(io.LimitReader(selected, 1<<20)).Decode(&snapshot); err != nil {
		return Snapshot{}, errors.New("cannot read watcher snapshot")
	}
	return snapshot, nil
}
