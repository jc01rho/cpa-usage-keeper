package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/service"
	servicedto "cpa-usage-keeper/internal/service/dto"
)

type instanceQueryUsageStub struct {
	lastFilter servicedto.UsageFilter
}

func (s *instanceQueryUsageStub) GetUsageOverview(_ context.Context, filter servicedto.UsageFilter) (*servicedto.UsageOverviewSnapshot, error) {
	s.lastFilter = filter
	return nil, nil
}
func (s *instanceQueryUsageStub) GetUsageActivity(context.Context, servicedto.UsageFilter) (*servicedto.UsageActivitySnapshot, error) {
	return nil, nil
}
func (s *instanceQueryUsageStub) GetUsageOverviewRealtime(context.Context, servicedto.UsageFilter) (*servicedto.UsageOverviewRealtime, error) {
	return nil, nil
}
func (s *instanceQueryUsageStub) ListUsageEvents(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventsPage, error) {
	return &servicedto.UsageEventsPage{}, nil
}
func (s *instanceQueryUsageStub) StreamUsageEvents(context.Context, servicedto.UsageFilter, func(servicedto.UsageEventRecord) error) error {
	return nil
}
func (s *instanceQueryUsageStub) ListUsageEventFilterOptions(context.Context, servicedto.UsageFilter) (*servicedto.UsageEventFilterOptions, error) {
	return &servicedto.UsageEventFilterOptions{}, nil
}
func (s *instanceQueryUsageStub) GetAnalysis(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisSnapshot, error) {
	return nil, nil
}
func (s *instanceQueryUsageStub) GetAnalysisLatency(context.Context, servicedto.UsageFilter) (*servicedto.AnalysisLatencyDiagnostics, error) {
	return nil, nil
}

type instanceQueryProvider struct {
	usageAPIAuthProvider
	instances map[string]entities.CPAInstance
}

func (p instanceQueryProvider) Get(_ context.Context, instanceID string) (entities.CPAInstance, error) {
	instance, ok := p.instances[instanceID]
	if !ok {
		return entities.CPAInstance{}, service.ErrInstanceNotFound
	}
	return instance, nil
}

func TestInstanceFilterMiddlewareValidatesSpecificAndAllScopes(t *testing.T) {
	enabledID := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb11"
	disabledID := "0198aa10-4d88-7a20-8f4e-8c8de4a9cb22"
	provider := instanceQueryProvider{instances: map[string]entities.CPAInstance{
		enabledID:  {ID: enabledID, DisplayName: "A", Enabled: true},
		disabledID: {ID: disabledID, DisplayName: "B", Enabled: false},
	}}
	usage := &instanceQueryUsageStub{}
	router := NewRouter(nil, nil, usage, nil, AuthConfig{}, nil, "", OptionalProviders{CPAInstances: provider})

	for _, tc := range []struct {
		name       string
		query      string
		wantStatus int
		wantFilter string
	}{
		{name: "omitted selector is legacy", wantStatus: http.StatusOK, wantFilter: entities.LegacyCPAInstanceID},
		{name: "explicit all", query: "instance_id=all", wantStatus: http.StatusOK, wantFilter: ""},
		{name: "enabled instance", query: "instance_id=" + enabledID, wantStatus: http.StatusOK, wantFilter: enabledID},
		{name: "unknown instance", query: "instance_id=0198aa10-4d88-7a20-8f4e-8c8de4a9cb33", wantStatus: http.StatusNotFound},
		{name: "disabled instance", query: "instance_id=" + disabledID, wantStatus: http.StatusConflict},
		{name: "duplicate selector", query: "instance_id=" + enabledID + "&instance_id=all", wantStatus: http.StatusBadRequest},
		{name: "malformed selector", query: "instance_id=not-a-uuid", wantStatus: http.StatusBadRequest},
		{name: "all must be canonical", query: "instance_id=ALL", wantStatus: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			usage.lastFilter = servicedto.UsageFilter{InstanceID: "unset"}
			path := "/api/v1/usage/overview?range=24h"
			if tc.query != "" {
				path += "&" + tc.query
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != tc.wantStatus {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if tc.wantStatus == http.StatusOK && usage.lastFilter.InstanceID != tc.wantFilter {
				t.Fatalf("filter=%q want=%q", usage.lastFilter.InstanceID, tc.wantFilter)
			}
		})
	}
}
