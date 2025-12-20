package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/youssefsiam38/agentpg/driver"
)

// CompactionEventListParams contains parameters for listing compaction events.
type CompactionEventListParams struct {
	SessionID *uuid.UUID
	Limit     int
	Offset    int
}

// ListCompactionEvents returns a list of compaction events.
func (s *Service[TTx]) ListCompactionEvents(ctx context.Context, params CompactionEventListParams) ([]*CompactionEventSummary, error) {
	if params.Limit <= 0 {
		params.Limit = 25
	}

	var events []*driver.CompactionEvent
	var err error

	if params.SessionID != nil {
		events, err = s.store.GetCompactionEvents(ctx, *params.SessionID, params.Limit+params.Offset)
	} else {
		// Get all compaction events - not supported without session filter
		// Return empty for now
		events = []*driver.CompactionEvent{}
	}

	if err != nil {
		return nil, err
	}

	// Apply offset and limit
	start := params.Offset
	if start > len(events) {
		start = len(events)
	}
	end := start + params.Limit
	if end > len(events) {
		end = len(events)
	}
	events = events[start:end]

	// Convert to summaries
	summaries := make([]*CompactionEventSummary, 0, len(events))
	for _, event := range events {
		var duration *time.Duration
		if event.DurationMS != nil && *event.DurationMS > 0 {
			d := time.Duration(*event.DurationMS) * time.Millisecond
			duration = &d
		}

		var reduction float64
		if event.OriginalTokens > 0 {
			reduction = 1.0 - (float64(event.CompactedTokens) / float64(event.OriginalTokens))
		}

		summaryCreated := event.SummaryContent != nil && *event.SummaryContent != ""

		summaries = append(summaries, &CompactionEventSummary{
			ID:              event.ID,
			SessionID:       event.SessionID,
			Strategy:        event.Strategy,
			OriginalTokens:  event.OriginalTokens,
			CompactedTokens: event.CompactedTokens,
			TokenReduction:  reduction,
			MessagesRemoved: event.MessagesRemoved,
			SummaryCreated:  summaryCreated,
			Duration:        duration,
			CreatedAt:       event.CreatedAt,
		})
	}

	return summaries, nil
}

// GetCompactionEventDetail returns detailed information about a compaction event.
// Note: This finds the event by iterating through session events since there's no direct lookup by ID.
func (s *Service[TTx]) GetCompactionEventDetail(ctx context.Context, sessionID, eventID uuid.UUID) (*CompactionEventDetail, error) {
	events, err := s.store.GetCompactionEvents(ctx, sessionID, 100)
	if err != nil {
		return nil, err
	}

	var event *driver.CompactionEvent
	for _, e := range events {
		if e.ID == eventID {
			event = e
			break
		}
	}

	if event == nil {
		return nil, ErrNotFound
	}

	detail := &CompactionEventDetail{
		Event: event,
	}

	// Calculate derived fields
	if event.OriginalTokens > 0 {
		detail.TokenReduction = 1.0 - (float64(event.CompactedTokens) / float64(event.OriginalTokens))
	}

	if event.DurationMS != nil && *event.DurationMS > 0 {
		d := time.Duration(*event.DurationMS) * time.Millisecond
		detail.Duration = &d
	}

	// Get session info
	session, err := s.store.GetSession(ctx, event.SessionID)
	if err == nil {
		detail.Session = &SessionSummary{
			ID:              session.ID,
			TenantID:        session.TenantID,
			Identifier:      session.Identifier,
			Depth:           session.Depth,
			CompactionCount: session.CompactionCount,
			CreatedAt:       session.CreatedAt,
		}
	}

	// Note: ArchivedMessageIDs would require GetArchivedMessages which isn't in the driver interface
	// Leave as empty for now

	return detail, nil
}

// GetSessionCompactionHistory returns compaction events for a session.
func (s *Service[TTx]) GetSessionCompactionHistory(ctx context.Context, sessionID uuid.UUID) ([]*CompactionEventSummary, error) {
	events, err := s.store.GetCompactionEvents(ctx, sessionID, 100)
	if err != nil {
		return nil, err
	}

	summaries := make([]*CompactionEventSummary, 0, len(events))
	for _, event := range events {
		var duration *time.Duration
		if event.DurationMS != nil && *event.DurationMS > 0 {
			d := time.Duration(*event.DurationMS) * time.Millisecond
			duration = &d
		}

		var reduction float64
		if event.OriginalTokens > 0 {
			reduction = 1.0 - (float64(event.CompactedTokens) / float64(event.OriginalTokens))
		}

		summaryCreated := event.SummaryContent != nil && *event.SummaryContent != ""

		summaries = append(summaries, &CompactionEventSummary{
			ID:              event.ID,
			SessionID:       event.SessionID,
			Strategy:        event.Strategy,
			OriginalTokens:  event.OriginalTokens,
			CompactedTokens: event.CompactedTokens,
			TokenReduction:  reduction,
			MessagesRemoved: event.MessagesRemoved,
			SummaryCreated:  summaryCreated,
			Duration:        duration,
			CreatedAt:       event.CreatedAt,
		})
	}

	return summaries, nil
}

// GetCompactionStats returns overall compaction statistics.
func (s *Service[TTx]) GetCompactionStats(ctx context.Context) (*CompactionStats, error) {
	driverStats, err := s.store.GetCompactionStats(ctx)
	if err != nil {
		return nil, err
	}

	return &CompactionStats{
		TotalCompactions:      driverStats.TotalCompactions,
		TotalTokensSaved:      driverStats.TotalTokensSaved,
		TotalMessagesArchived: driverStats.TotalMessagesArchived,
		AvgReductionPercent:   driverStats.AvgReductionPercent,
	}, nil
}
