package trader

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"brale/internal/gateway/database"
)

type SQLiteEventStore struct {
	db database.LivePositionStore
}

func NewSQLiteEventStore(db database.LivePositionStore) *SQLiteEventStore {
	return &SQLiteEventStore{
		db: db,
	}
}

func (s *SQLiteEventStore) Append(evt EventEnvelope) error {
	if s.db == nil {
		return fmt.Errorf("sqlite store: database is nil")
	}

	if evt.ID == "" {
		evt.ID = fmt.Sprintf("evt-%d", time.Now().UnixNano())
	}

	rec := database.EventRecord{
		ID:        evt.ID,
		Type:      string(evt.Type),
		Payload:   []byte(evt.Payload),
		CreatedAt: evt.CreatedAt,
		TradeID:   evt.TradeID,
		Symbol:    evt.Symbol,
	}

	return s.db.AppendEvent(context.Background(), rec)
}

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

func (s *SQLiteEventStore) Close() error {
	return nil
}
