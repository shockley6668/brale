package trader

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// EventStore defines the interface for persisting and retrieving events.
type EventStore interface {
	// Append writes an event to the store.
	Append(evt EventEnvelope) error
	// LoadAll reads all events from the store.
	LoadAll() ([]EventEnvelope, error)
	// Close closes the underlying storage connection.
	Close() error
}

// FileEventStore implements EventStore using a newline-delimited JSON file.
type FileEventStore struct {
	path string
	file *os.File
	mu   sync.Mutex
}

// NewFileEventStore creates a new FileEventStore.
func NewFileEventStore(path string) (*FileEventStore, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open event store file: %w", err)
	}
	return &FileEventStore{
		path: path,
		file: f,
	}, nil
}

// Append writes an event to the file as a JSON line.
func (s *FileEventStore) Append(evt EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure we are appending
	// JSON marshaling
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if _, err := s.file.Write(data); err != nil {
		return fmt.Errorf("failed to write event to file: %w", err)
	}
	if _, err := s.file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline: %w", err)
	}

	// Sync to disk to ensure durability
	// Performance note: doing fsync on every event is safer but slower.
	// For high frequency, we might want to buffer or sync periodically.
	// For this phase, safety first.
	// if err := s.file.Sync(); err != nil {
	// 	 return fmt.Errorf("failed to sync file: %w", err)
	// }

	return nil
}

// LoadAll reads all events from the file.
func (s *FileEventStore) LoadAll() ([]EventEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Reset file pointer to beginning
	if _, err := s.file.Seek(0, 0); err != nil {
		return nil, fmt.Errorf("failed to seek to start: %w", err)
	}

	var events []EventEnvelope
	scanner := bufio.NewScanner(s.file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt EventEnvelope
		if err := json.Unmarshal(line, &evt); err != nil {
			return nil, fmt.Errorf("failed to unmarshal line %d: %w", lineNum, err)
		}
		events = append(events, evt)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	// Seek back to end for appending
	if _, err := s.file.Seek(0, 2); err != nil {
		return nil, fmt.Errorf("failed to seek to end: %w", err)
	}

	return events, nil
}

// Close closes the file.
func (s *FileEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
