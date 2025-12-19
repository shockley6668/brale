package trader

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type EventStore interface {
	Append(evt EventEnvelope) error

	LoadAll() ([]EventEnvelope, error)

	Close() error
}

type FileEventStore struct {
	path string
	file *os.File
	mu   sync.Mutex
}

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

func (s *FileEventStore) Append(evt EventEnvelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	return nil
}

func (s *FileEventStore) LoadAll() ([]EventEnvelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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

	if _, err := s.file.Seek(0, 2); err != nil {
		return nil, fmt.Errorf("failed to seek to end: %w", err)
	}

	return events, nil
}

func (s *FileEventStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.Close()
}
