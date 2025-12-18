package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"brale/internal/gateway/database"
)

// SQLiteEventStore implements EventStore using the main SQLite database.
type SQLiteEventStore struct {
	db database.LivePositionStore
}

// NewSQLiteEventStore creates a new adapter.
func NewSQLiteEventStore(db database.LivePositionStore) *SQLiteEventStore {
	return &SQLiteEventStore{
		db: db,
	}
}

// Append writes an event to the SQLite database.
func (s *SQLiteEventStore) Append(evt EventEnvelope) error {
	if s.db == nil {
		return fmt.Errorf("sqlite store: database is nil")
	}

	// Default ID if missing
	if evt.ID == "" {
		// We might want to generate UUID here, or assume actor did it.
		// For now let's hope actor did it or DB can auto-increment?
		// EventRecord.ID is string (UUID). If empty, constraint violation?
		// Assuming Actor assigns ID.
	}

	rec := database.EventRecord{
		ID:        evt.ID,
		Type:      string(evt.Type),
		Payload:   []byte(evt.Payload),
		CreatedAt: evt.CreatedAt,
		TradeID:   evt.TradeID,
		Symbol:    evt.Symbol,
	}

	// Use background context for now, or trace context if we had it stored in Envelope?
	return s.db.AppendEvent(context.Background(), rec)
}

// LoadAll reads all events from the database.
// Note: In production we might want LoadSince(id) or snapshotting.
// Here we load all history which is acceptable for modest event counts.
func (s *SQLiteEventStore) LoadAll() ([]EventEnvelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("sqlite store: database is nil")
	}

	ctx := context.Background()
	limit := 1000
	since := time.Time{}
	var out []EventEnvelope
	for {
		recs, err := s.db.LoadEvents(ctx, since, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to load events from sqlite: %w", err)
		}
		if len(recs) == 0 {
			break
		}
		for _, r := range recs {
			out = append(out, EventEnvelope{
				ID:        r.ID,
				Type:      EventType(r.Type),
				Payload:   json.RawMessage(r.Payload),
				CreatedAt: r.CreatedAt,
				TradeID:   r.TradeID,
				Symbol:    r.Symbol,
			})
		}
		since = recs[len(recs)-1].CreatedAt
		if len(recs) < limit {
			break
		}
	}
	return out, nil
}

// Close is a no-op as the DB is managed externally (by Manager/App).
func (s *SQLiteEventStore) Close() error {
	return nil
}
