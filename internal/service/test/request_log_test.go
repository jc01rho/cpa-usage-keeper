package test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/cpa"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
	"gorm.io/gorm"
)

type requestLogClientStub struct {
	mu     sync.Mutex
	calls  int
	result *cpa.RequestLogResult
	err    error

	downloadCalls  int
	downloadResult *cpa.RequestLogStream
	downloadErr    error
	started        chan struct{}
	block          chan struct{}
}

func (s *requestLogClientStub) FetchRequestLogByID(ctx context.Context, _ string) (*cpa.RequestLogResult, error) {
	s.mu.Lock()
	s.calls++
	result := s.result
	err := s.err
	started := s.started
	block := s.block
	s.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return result, err
}

func (s *requestLogClientStub) fetchCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *requestLogClientStub) rawDownloadCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.downloadCalls
}

func (s *requestLogClientStub) OpenRequestLogByID(context.Context, string) (*cpa.RequestLogStream, error) {
	s.mu.Lock()
	s.downloadCalls++
	result := s.result
	err := s.err
	downloadResult := s.downloadResult
	downloadErr := s.downloadErr
	s.mu.Unlock()
	if downloadResult != nil || downloadErr != nil {
		return downloadResult, downloadErr
	}
	if result == nil {
		return nil, err
	}
	return &cpa.RequestLogStream{
		StatusCode:    result.StatusCode,
		Filename:      result.Filename,
		ContentType:   result.ContentType,
		ContentLength: int64(len(result.Body)),
		Body:          io.NopCloser(bytes.NewReader(result.Body)),
	}, err
}

func TestRequestLogServiceLoadsEventLogAndCachesByRequestID(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:    "event-1",
		RequestID:   "req-log-1",
		Timestamp:   time.Date(2026, 7, 8, 12, 0, 0, 0, time.Local),
		Model:       "claude-sonnet",
		Source:      "source",
		AuthIndex:   "auth-1",
		APIGroupKey: "group",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Filename:   "v1-responses-req-log-1.log",
		Body:       []byte("=== REQUEST INFO ===\nURL: /v1/responses\n=== API RESPONSE ===\n{\"ok\":true}\n"),
	}}
	provider := service.NewRequestLogService(db, client)

	first, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if first.RequestID != "req-log-1" || first.Filename != "v1-responses-req-log-1.log" || first.Cached {
		t.Fatalf("unexpected first response: %+v", first)
	}
	if len(first.Sections) != 2 || first.Sections[0].Title != "REQUEST INFO" || first.Sections[1].Title != "API RESPONSE" {
		t.Fatalf("unexpected sections: %+v", first.Sections)
	}

	second, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("cached GetUsageEventRequestLog returned error: %v", err)
	}
	if !second.Cached {
		t.Fatalf("expected cached response, got %+v", second)
	}
	if client.calls != 1 {
		t.Fatalf("expected one CPA call, got %d", client.calls)
	}
}

func TestRequestLogServiceCachesRawOnlyAndReparsesSections(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-raw-cache",
		RequestID: "req-raw-cache",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Filename:   "raw-cache.log",
		Body:       []byte("=== REQUEST INFO ===\nURL: /v1/responses\n=== API RESPONSE ===\n{\"ok\":true}\n"),
	}}
	provider := service.NewRequestLogService(db, client)

	first, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if len(first.Sections) != 2 {
		t.Fatalf("expected parsed sections, got %+v", first.Sections)
	}
	first.Sections[0].Content = "mutated cached section"

	second, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("cached GetUsageEventRequestLog returned error: %v", err)
	}
	if len(second.Sections) == 0 || second.Sections[0].Content != "URL: /v1/responses" {
		t.Fatalf("expected cached raw log to be reparsed into fresh sections, got %+v", second.Sections)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected one CPA call, got %d", client.fetchCalls())
	}
}

func TestRequestLogServiceMapsCPANotFoundToUnavailable(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-404",
		RequestID: "req-missing",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result: &cpa.RequestLogResult{StatusCode: http.StatusNotFound, Body: []byte(`{"error":"missing"}`)},
		err:    errors.New("management request log request returned status 404"),
	}
	provider := service.NewRequestLogService(db, client)

	_, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected ErrRequestLogUnavailable, got %v", err)
	}

	_, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected cached ErrRequestLogUnavailable, got %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("expected 404 response to be negative cached, got %d calls", client.calls)
	}
}

func TestRequestLogServiceNegativeCacheExpiresAfterTTL(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-404-expire",
		RequestID: "req-missing-expire",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	client := &requestLogClientStub{
		result: &cpa.RequestLogResult{StatusCode: http.StatusNotFound, Body: []byte(`{"error":"missing"}`)},
		err:    errors.New("management request log request returned status 404"),
	}
	provider := service.NewRequestLogService(db, client)
	setRequestLogServiceNow(t, provider, func() time.Time { return now })

	_, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected ErrRequestLogUnavailable, got %v", err)
	}
	_, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if !errors.Is(err, service.ErrRequestLogUnavailable) {
		t.Fatalf("expected cached ErrRequestLogUnavailable, got %v", err)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected cached 404 before TTL, got %d calls", client.fetchCalls())
	}

	now = now.Add(11 * time.Second)
	client.mu.Lock()
	client.result = &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nrecovered\n")}
	client.err = nil
	client.mu.Unlock()
	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected expired negative cache to refetch successfully, got %v", err)
	}
	if response.Sections[0].Content != "recovered" {
		t.Fatalf("unexpected refetched response: %+v", response)
	}
	if client.fetchCalls() != 2 {
		t.Fatalf("expected negative cache to expire and refetch, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceDeductsExpiredCacheEntryBytes(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-expired-bytes",
		RequestID: "req-expired-bytes",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	raw := "=== REQUEST INFO ===\n" + strings.Repeat("x", 1024) + "\n"
	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Filename:   "expired-bytes.log",
		Body:       []byte(raw),
	}}
	provider := service.NewRequestLogService(db, client)
	setRequestLogServiceNow(t, provider, func() time.Time { return now })

	if _, err := provider.GetUsageEventRequestLog(context.Background(), 1); err != nil {
		t.Fatalf("load request log: %v", err)
	}
	firstBytes := requestLogServiceCacheBytes(t, provider)
	if firstBytes <= 0 {
		t.Fatalf("expected cached bytes to be positive, got %d", firstBytes)
	}

	now = now.Add(11 * time.Minute)
	if _, err := provider.GetUsageEventRequestLog(context.Background(), 1); err != nil {
		t.Fatalf("reload expired request log: %v", err)
	}
	secondBytes := requestLogServiceCacheBytes(t, provider)
	if secondBytes != firstBytes {
		t.Fatalf("expected expired cache entry bytes to be deducted before refetch, got first=%d second=%d", firstBytes, secondBytes)
	}
}

func TestRequestLogServiceCoalescesConcurrentPreviewMisses(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight",
		RequestID: "req-singleflight",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\ncoalesced\n")},
		started: make(chan struct{}, 2),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
			if err == nil && response.Sections[0].Content != "coalesced" {
				err = errors.New("unexpected response content")
			}
			errs <- err
		}()
	}
	<-client.started
	time.Sleep(20 * time.Millisecond)
	close(client.block)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent GetUsageEventRequestLog returned error: %v", err)
		}
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected concurrent miss to fetch once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceCoalescedPreviewMissIgnoresLeaderCancellation(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight-leader-cancel",
		RequestID: "req-singleflight-leader-cancel",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nleader survived\n")},
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		response, err := provider.GetUsageEventRequestLog(leaderCtx, 1)
		if err == nil && response.Sections[0].Content != "leader survived" {
			err = errors.New("unexpected leader response content")
		}
		leaderErr <- err
	}()
	<-client.started
	cancelLeader()

	followerErr := make(chan error, 1)
	go func() {
		response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
		if err == nil && response.Sections[0].Content != "leader survived" {
			err = errors.New("unexpected follower response content")
		}
		followerErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(client.block)

	if err := <-leaderErr; err != nil {
		t.Fatalf("expected leader cancellation not to cancel shared fetch, got %v", err)
	}
	if err := <-followerErr; err != nil {
		t.Fatalf("expected follower to receive shared fetch result, got %v", err)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected shared fetch to run once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceCoalescedPreviewFollowerCanCancelIndependently(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-singleflight-follower-cancel",
		RequestID: "req-singleflight-follower-cancel",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{
		result:  &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nfollower survived\n")},
		started: make(chan struct{}, 1),
		block:   make(chan struct{}),
	}
	provider := service.NewRequestLogService(db, client)
	leaderErr := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(context.Background(), 1)
		leaderErr <- err
	}()
	<-client.started

	followerCtx, cancelFollower := context.WithCancel(context.Background())
	followerErr := make(chan error, 1)
	go func() {
		_, err := provider.GetUsageEventRequestLog(followerCtx, 1)
		followerErr <- err
	}()
	cancelFollower()
	if err := <-followerErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected follower to stop waiting on context cancellation, got %v", err)
	}

	close(client.block)
	if err := <-leaderErr; err != nil {
		t.Fatalf("expected leader fetch to complete after follower cancellation, got %v", err)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected shared fetch to run once, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceHandlesLargePreviewAsDownloadable(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-large",
		RequestID: "req-large",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:    http.StatusOK,
		Filename:      "large-request.log",
		Body:          make([]byte, service.RequestLogPreviewMaxBytes()+1),
		BodyTruncated: true,
		ContentType:   "text/plain",
		ContentLength: int64(service.RequestLogPreviewMaxBytes() + 1),
	}}
	provider := service.NewRequestLogService(db, client)

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.TooLarge || !response.Downloadable || response.Previewable || len(response.Sections) != 0 {
		t.Fatalf("unexpected large preview response: %+v", response)
	}
	if response.Filename != "large-request.log" {
		t.Fatalf("unexpected filename %q", response.Filename)
	}
}

func TestRequestLogServiceTooLargePreviewCacheExpiresAfterNegativeTTL(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-large-expire",
		RequestID: "req-large-expire",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:    http.StatusOK,
		Filename:      "large-request.log",
		BodyTruncated: true,
		ContentType:   "text/plain",
		ContentLength: int64(service.RequestLogPreviewMaxBytes() + 1),
	}}
	provider := service.NewRequestLogService(db, client)
	setRequestLogServiceNow(t, provider, func() time.Time { return now })

	response, err := provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.TooLarge {
		t.Fatalf("expected initial too-large response, got %+v", response)
	}
	now = now.Add(9 * time.Second)
	response, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("cached GetUsageEventRequestLog returned error: %v", err)
	}
	if !response.Cached || !response.TooLarge {
		t.Fatalf("expected too-large response to be cached before 10s TTL, got %+v", response)
	}
	if client.fetchCalls() != 1 {
		t.Fatalf("expected cached too-large response before TTL, got %d calls", client.fetchCalls())
	}

	now = now.Add(2 * time.Second)
	client.mu.Lock()
	client.result = &cpa.RequestLogResult{StatusCode: http.StatusOK, Body: []byte("=== RAW LOG ===\nsmall again\n")}
	client.mu.Unlock()
	response, err = provider.GetUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected expired too-large cache to refetch successfully, got %v", err)
	}
	if response.TooLarge || len(response.Sections) != 1 || response.Sections[0].Content != "small again" {
		t.Fatalf("expected refetched preview response, got %+v", response)
	}
	if client.fetchCalls() != 2 {
		t.Fatalf("expected too-large cache to expire after 10s and refetch, got %d calls", client.fetchCalls())
	}
}

func TestRequestLogServiceDownloadUsesCachedPreviewRawBody(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-cached-download",
		RequestID: "req-cached-download",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode:  http.StatusOK,
		Filename:    "cached-download.log",
		ContentType: "text/plain; charset=utf-8",
		Body:        []byte("=== RAW LOG ===\nfrom cache\n"),
	}}
	provider := service.NewRequestLogService(db, client)
	if _, err := provider.GetUsageEventRequestLog(context.Background(), 1); err != nil {
		t.Fatalf("load request log: %v", err)
	}

	download, err := provider.DownloadUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("DownloadUsageEventRequestLog returned error: %v", err)
	}
	body, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	_ = download.Body.Close()
	if string(body) != "=== RAW LOG ===\nfrom cache\n" {
		t.Fatalf("unexpected cached download body %q", string(body))
	}
	if download.Filename != "cached-download.log" || download.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected cached download metadata: %+v", download)
	}
	if client.rawDownloadCalls() != 0 {
		t.Fatalf("expected cached small log download not to call raw CPA stream, got %d", client.rawDownloadCalls())
	}
}

func TestRequestLogServicePrunesCacheByTotalBytes(t *testing.T) {
	db := openRequestLogTestDB(t)
	eventCount := 21
	events := make([]entities.UsageEvent, 0, eventCount)
	for i := 0; i < eventCount; i++ {
		events = append(events, entities.UsageEvent{
			EventKey:  "event-prune-" + strconv.Itoa(i),
			RequestID: "req-prune-" + strconv.Itoa(i),
		})
	}
	if _, _, err := repository.InsertUsageEvents(db, events); err != nil {
		t.Fatalf("insert usage events: %v", err)
	}

	client := &requestLogClientStub{result: &cpa.RequestLogResult{
		StatusCode: http.StatusOK,
		Filename:   "request.log",
		Body:       []byte("=== REQUEST INFO ===\n" + strings.Repeat("x", 5*1024*1024-64)),
	}}
	provider := service.NewRequestLogService(db, client)

	for eventID := int64(1); eventID <= int64(len(events)); eventID++ {
		if _, err := provider.GetUsageEventRequestLog(context.Background(), eventID); err != nil {
			t.Fatalf("load request log for event %d: %v", eventID, err)
		}
	}
	if client.calls != len(events) {
		t.Fatalf("expected initial CPA calls to match event count, got %d", client.calls)
	}

	if _, err := provider.GetUsageEventRequestLog(context.Background(), 1); err != nil {
		t.Fatalf("reload pruned request log: %v", err)
	}
	if client.calls != len(events)+1 {
		t.Fatalf("expected oldest byte-budget cache entry to be pruned and refetched, got %d calls", client.calls)
	}
}

func TestRequestLogServiceDownloadFetchesRawBody(t *testing.T) {
	db := openRequestLogTestDB(t)
	if _, _, err := repository.InsertUsageEvents(db, []entities.UsageEvent{{
		EventKey:  "event-download",
		RequestID: "req-download",
	}}); err != nil {
		t.Fatalf("insert usage event: %v", err)
	}

	client := &requestLogClientStub{downloadResult: &cpa.RequestLogStream{
		StatusCode:    http.StatusOK,
		Filename:      "download.log",
		ContentType:   "text/plain; charset=utf-8",
		ContentLength: 7,
		Body:          io.NopCloser(bytes.NewBufferString("raw log")),
	}}
	provider := service.NewRequestLogService(db, client)
	downloader, ok := provider.(interface {
		DownloadUsageEventRequestLog(context.Context, int64) (service.RequestLogDownload, error)
	})
	if !ok {
		t.Fatalf("request log provider does not support downloads")
	}

	download, err := downloader.DownloadUsageEventRequestLog(context.Background(), 1)
	if err != nil {
		t.Fatalf("DownloadUsageEventRequestLog returned error: %v", err)
	}
	body, err := io.ReadAll(download.Body)
	if err != nil {
		t.Fatalf("read download body: %v", err)
	}
	_ = download.Body.Close()
	if string(body) != "raw log" || download.Filename != "download.log" || download.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("unexpected download response: %+v", download)
	}
	if client.downloadCalls != 1 {
		t.Fatalf("expected one raw download call, got %d", client.downloadCalls)
	}
}

func openRequestLogTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "request-log.db")})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql database: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Fatalf("close database: %v", err)
		}
	})
	return db
}

func setRequestLogServiceNow(t *testing.T, provider service.RequestLogProvider, now func() time.Time) {
	t.Helper()
	setRequestLogServiceField(t, provider, "now", reflect.ValueOf(now))
}

func requestLogServiceCacheBytes(t *testing.T, provider service.RequestLogProvider) int {
	t.Helper()
	field := requestLogServiceField(t, provider, "cacheBytes")
	return int(field.Int())
}

func setRequestLogServiceField(t *testing.T, provider service.RequestLogProvider, name string, value reflect.Value) {
	t.Helper()
	field := requestLogServiceField(t, provider, name)
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(value)
}

func requestLogServiceField(t *testing.T, provider service.RequestLogProvider, name string) reflect.Value {
	t.Helper()
	value := reflect.ValueOf(provider)
	if value.Kind() != reflect.Ptr || value.IsNil() {
		t.Fatalf("expected request log service pointer, got %T", provider)
	}
	field := value.Elem().FieldByName(name)
	if !field.IsValid() {
		t.Fatalf("request log service field %q not found", name)
	}
	return field
}
