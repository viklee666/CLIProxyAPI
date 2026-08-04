package usage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
	usageparser "github.com/seakee/cpa-manager-plus/apps/manager-server/internal/usage"
)

type ImportResult struct {
	Format      string   `json:"format"`
	Added       int      `json:"added"`
	Skipped     int      `json:"skipped"`
	Total       int      `json:"total"`
	Failed      int      `json:"failed"`
	Unsupported int      `json:"unsupported"`
	Warnings    []string `json:"warnings"`
}

type ImportPersistenceError struct {
	err error
}

func (e *ImportPersistenceError) Error() string {
	return fmt.Sprintf("persist usage import batch: %v", e.err)
}

func (e *ImportPersistenceError) Unwrap() error {
	return e.err
}

type Service struct {
	store                  *store.Store
	notifierMu             sync.RWMutex
	eventsInsertedNotifier func()
}

const importBatchSize = 256

var ErrInvalidUsageCursor = errors.New("invalid usage cursor")

type CompatibleUsageRequest struct {
	Page     int
	PageSize int
	Cursor   string
	// APIKeyHashes scopes a tenant usage view. nil preserves management usage;
	// an empty non-nil value deliberately produces an empty result.
	APIKeyHashes []string
}

type CompatibleUsageResponse struct {
	usageparser.Payload
	Page       int    `json:"page"`
	PageSize   int    `json:"page_size"`
	Cursor     string `json:"cursor,omitempty"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"total_pages"`
	HasMore    bool   `json:"has_more"`
}

type compatibleUsageCursor struct {
	Version           int   `json:"v"`
	SnapshotMaxID     int64 `json:"max_id"`
	BeforeTimestampMS int64 `json:"before_ms"`
	BeforeID          int64 `json:"before_id"`
}

func New(store *store.Store) *Service {
	return &Service{store: store}
}

func (s *Service) SetEventsInsertedNotifier(notifier func()) {
	s.notifierMu.Lock()
	s.eventsInsertedNotifier = notifier
	s.notifierMu.Unlock()
}

func (s *Service) notifyEventsInserted() {
	s.notifierMu.RLock()
	notifier := s.eventsInsertedNotifier
	s.notifierMu.RUnlock()
	if notifier != nil {
		notifier()
	}
}

func (s *Service) WriteCompatibleUsage(ctx context.Context, writer io.Writer, limit int) error {
	return s.store.WriteCompatibleUsage(ctx, writer, limit)
}

func (s *Service) CompatibleUsage(ctx context.Context, req CompatibleUsageRequest) (CompatibleUsageResponse, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 200
	}
	if req.PageSize > 500 {
		req.PageSize = 500
	}

	query := store.RecentUsagePageQuery{PageSize: req.PageSize, APIKeyHashes: req.APIKeyHashes}
	if cursor := strings.TrimSpace(req.Cursor); cursor != "" {
		decoded, err := decodeCompatibleUsageCursor(cursor)
		if err != nil {
			return CompatibleUsageResponse{}, err
		}
		query.SnapshotMaxID = decoded.SnapshotMaxID
		query.BeforeTimestampMS = decoded.BeforeTimestampMS
		query.BeforeID = decoded.BeforeID
	} else {
		query.Offset = int64(req.Page-1) * int64(req.PageSize)
	}
	page, err := s.store.RecentUsagePage(ctx, query)
	if err != nil {
		return CompatibleUsageResponse{}, err
	}

	nextCursor := ""
	if page.HasMore && page.LastTimestampMS > 0 && page.LastID > 0 {
		nextCursor, err = encodeCompatibleUsageCursor(compatibleUsageCursor{
			Version:           1,
			SnapshotMaxID:     page.SnapshotMaxID,
			BeforeTimestampMS: page.LastTimestampMS,
			BeforeID:          page.LastID,
		})
		if err != nil {
			return CompatibleUsageResponse{}, err
		}
	}
	totalPages := 0
	if page.Total > 0 {
		totalPages = int((page.Total + int64(req.PageSize) - 1) / int64(req.PageSize))
	}
	return CompatibleUsageResponse{
		Payload:    usageparser.BuildPayload(page.Items),
		Page:       req.Page,
		PageSize:   req.PageSize,
		Cursor:     strings.TrimSpace(req.Cursor),
		NextCursor: nextCursor,
		Total:      page.Total,
		TotalPages: totalPages,
		HasMore:    page.HasMore,
	}, nil
}

func encodeCompatibleUsageCursor(cursor compatibleUsageCursor) (string, error) {
	encoded, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCompatibleUsageCursor(raw string) (compatibleUsageCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(raw))
	if err != nil {
		return compatibleUsageCursor{}, ErrInvalidUsageCursor
	}
	var cursor compatibleUsageCursor
	if err := json.Unmarshal(decoded, &cursor); err != nil || cursor.Version != 1 || cursor.SnapshotMaxID <= 0 || cursor.BeforeTimestampMS <= 0 || cursor.BeforeID <= 0 || cursor.BeforeID > cursor.SnapshotMaxID {
		return compatibleUsageCursor{}, ErrInvalidUsageCursor
	}
	return cursor, nil
}

func (s *Service) WriteExport(ctx context.Context, writer io.Writer, limit int) error {
	return s.store.WriteExportJSONL(ctx, writer, limit)
}

func (s *Service) Import(ctx context.Context, reader io.Reader) (ImportResult, *usageparser.ImportStreamResult, error) {
	var added int
	var skipped int
	parsed, err := usageparser.StreamImportPayload(reader, importBatchSize, func(events []usageparser.Event) error {
		result, err := s.store.InsertEvents(ctx, events)
		if err != nil {
			return &ImportPersistenceError{err: err}
		}
		added += result.Inserted
		skipped += result.Skipped
		return nil
	})
	if added > 0 {
		s.notifyEventsInserted()
	}
	result := ImportResult{
		Format:      parsed.Format,
		Added:       added,
		Skipped:     skipped,
		Total:       parsed.Total,
		Failed:      parsed.Failed,
		Unsupported: parsed.Unsupported,
		Warnings:    parsed.Warnings,
	}
	if err != nil {
		return result, &parsed, err
	}
	return result, &parsed, nil
}

func (s *Service) Counts(ctx context.Context) (events int64, deadLetters int64, err error) {
	return s.store.Counts(ctx)
}
