package repository

import (
	"time"

	"gorm.io/gorm"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/pricing"
	"cpa-usage-keeper/internal/repository/dto"
	"cpa-usage-keeper/internal/timeutil"
)

// analysisBoundaryEventProjectionColumns adds identity columns so boundary tokens reach the same
// credential/provider compositions as the stats passes.
const analysisBoundaryEventProjectionColumns = usageOverviewBoundaryEventProjectionColumns + ", instance_id, auth_index, latency_ms"

// analysisWindowAccumulator folds every source (daily stats, hourly stats, boundary events) of one
// exact window into a single AnalysisRecord.
type analysisWindowAccumulator struct {
	record           *dto.AnalysisRecord
	costResolver     pricing.Resolver
	bucketByDay      bool
	speed            *analysisModelSpeedAccumulator
	bucketTotals     map[time.Time]*dto.AnalysisTokenUsageBucketRecord
	modelUsageTotals map[analysisModelUsageKey]*dto.AnalysisModelUsageRecord
	apiTotals        map[string]*dto.AnalysisCompositionRecord
	modelTotals      map[string]*dto.AnalysisCompositionRecord
	credentialTotals map[string]*dto.AnalysisCompositionRecord
	aiProviderTotals map[string]*dto.AnalysisCompositionRecord
	providerTotals   map[string]*dto.AnalysisCompositionRecord
	heatmapTotals    map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord
}

func newAnalysisWindowAccumulator(record *dto.AnalysisRecord, costResolver pricing.Resolver, bucketByDay bool, start, end time.Time) *analysisWindowAccumulator {
	return &analysisWindowAccumulator{
		record:           record,
		costResolver:     costResolver,
		bucketByDay:      bucketByDay,
		speed:            newAnalysisModelSpeed(start, end, bucketByDay),
		bucketTotals:     map[time.Time]*dto.AnalysisTokenUsageBucketRecord{},
		modelUsageTotals: map[analysisModelUsageKey]*dto.AnalysisModelUsageRecord{},
		apiTotals:        map[string]*dto.AnalysisCompositionRecord{},
		modelTotals:      map[string]*dto.AnalysisCompositionRecord{},
		credentialTotals: map[string]*dto.AnalysisCompositionRecord{},
		aiProviderTotals: map[string]*dto.AnalysisCompositionRecord{},
		providerTotals:   map[string]*dto.AnalysisCompositionRecord{},
		heatmapTotals:    map[analysisHeatmapKey]*dto.AnalysisHeatmapRecord{},
	}
}

func (a *analysisWindowAccumulator) bucketOf(timestamp time.Time) time.Time {
	normalized := timeutil.NormalizeStorageTime(timestamp)
	if !a.bucketByDay {
		return normalized.Truncate(time.Hour)
	}
	day, _ := timeutil.DayBounds(timeutil.DateOf(normalized), normalized.Location())
	return day.Start
}

func (a *analysisWindowAccumulator) addStatRow(row analysisOverviewStatProjection, identityLookup analysisIdentityLookup) {
	bucket := a.bucketOf(row.BucketStart)
	cost, costAvailable := *row.CostUSD, *row.UnavailableCostCount == 0
	a.speed.add(row, bucket)
	modelName := resolveAnalysisModelName(a.costResolver, row)
	applyAnalysisRow(a.record, a.bucketTotals, a.modelUsageTotals, a.apiTotals, a.modelTotals, a.heatmapTotals, bucket, row.InstanceID, row.APIGroupKey, modelName, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
	applyAnalysisIdentityComposition(identityLookup, a.credentialTotals, a.aiProviderTotals, a.providerTotals, row.InstanceID, row.AuthIndex, row.RequestCount, row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheCreationTokens, row.ReasoningTokens, row.TotalTokens, cost, costAvailable)
}

func (a *analysisWindowAccumulator) addEvent(event entities.UsageEvent, identityLookup analysisIdentityLookup) error {
	cost, costAvailable, err := usageOverviewStoredEventCost(event)
	if err != nil {
		return err
	}
	bucket := a.bucketOf(event.Timestamp)
	modelName := a.costResolver.ResolveModelName(pricing.UsageDimensions{Model: event.Model})
	speed := analysisOverviewStatProjection{Model: event.Model, TotalTokens: event.TotalTokens}
	if event.OutputTokens > 0 && event.LatencyMS > 0 {
		speed.SpeedTPSSum = float64(event.OutputTokens) * 1000 / float64(event.LatencyMS)
		speed.SpeedSampleCount = 1
	}
	a.speed.add(speed, bucket)
	applyAnalysisRow(a.record, a.bucketTotals, a.modelUsageTotals, a.apiTotals, a.modelTotals, a.heatmapTotals, bucket, event.InstanceID, event.APIGroupKey, modelName, 1, event.InputTokens, event.OutputTokens, event.CacheReadTokens, event.CacheCreationTokens, event.ReasoningTokens, event.TotalTokens, cost, costAvailable)
	applyAnalysisIdentityComposition(identityLookup, a.credentialTotals, a.aiProviderTotals, a.providerTotals, event.InstanceID, event.AuthIndex, 1, event.InputTokens, event.OutputTokens, event.CacheReadTokens, event.CacheCreationTokens, event.ReasoningTokens, event.TotalTokens, cost, costAvailable)
	return nil
}

func (a *analysisWindowAccumulator) finalize() {
	a.record.ModelSpeed = a.speed.finalize()
	finalizeAnalysisRecord(a.record, a.bucketTotals, a.modelUsageTotals, a.apiTotals, a.modelTotals, a.credentialTotals, a.aiProviderTotals, a.providerTotals, a.heatmapTotals)
}

// buildAnalysisExactWindow aggregates the requested window with the same slicing as Overview:
// daily rollups for whole interior days, hourly rollups for the remaining whole hours, and raw
// events for the partial hours at both edges. Totals therefore match Overview for every range.
func buildAnalysisExactWindow(db *gorm.DB, filter dto.UsageQueryFilter, record *dto.AnalysisRecord, costResolver pricing.Resolver) error {
	effective := usageOverviewEffectiveFilter(filter, usageOverviewQueryNow(filter))
	if effective.StartTime == nil || effective.EndTime == nil {
		return nil
	}
	windowStart := timeutil.NormalizeStorageTime(*effective.StartTime)
	windowEnd := timeutil.NormalizeStorageTime(*effective.EndTime)
	record.RangeStart = &windowStart
	record.RangeEnd = &windowEnd
	if !windowEnd.After(windowStart) {
		return nil
	}

	bucketByDay := record.Granularity == dto.AnalysisGranularityDaily
	accumulator := newAnalysisWindowAccumulator(record, costResolver, bucketByDay, windowStart, windowEnd)
	fullStart, fullEnd := usageOverviewFullHourWindow(windowStart, windowEnd)

	// Partial hours at both edges cannot be served from whole-hour rollups; read the raw events.
	currentRight := usageOverviewCurrentRightBoundary(effective, *effective.QueryNow)
	boundaryEvents, err := loadAnalysisBoundaryEvents(db, effective, usageOverviewRawEventWindows(effective, fullStart, fullEnd, currentRight))
	if err != nil {
		return err
	}
	eventAuthIndexes := make([]string, 0, len(boundaryEvents))
	seenAuthIndex := make(map[string]struct{}, len(boundaryEvents))
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		if authIndex := event.AuthIndex; authIndex != "" {
			if _, ok := seenAuthIndex[authIndex]; !ok {
				seenAuthIndex[authIndex] = struct{}{}
				eventAuthIndexes = append(eventAuthIndexes, authIndex)
			}
		}
	}
	eventIdentityLookup, err := loadAnalysisIdentityLookup(db, effective.InstanceID, eventAuthIndexes)
	if err != nil {
		return err
	}
	for _, event := range boundaryEvents {
		if usageOverviewEventInsideWindow(event, fullStart, fullEnd) {
			continue
		}
		if err := accumulator.addEvent(event, eventIdentityLookup); err != nil {
			return err
		}
	}

	if !fullEnd.After(fullStart) {
		accumulator.finalize()
		fillAnalysisFullDayHourlyBuckets(record, filter)
		return nil
	}

	fullDayStart, fullDayEnd := usageOverviewFullDayWindow(fullStart, fullEnd)
	if !bucketByDay || !fullDayEnd.After(fullDayStart) {
		if err := accumulateAnalysisHourlyWindow(db, effective, accumulator, fullStart, fullEnd); err != nil {
			return err
		}
	} else {
		// Whole interior days come from daily rollups; the leftover whole hours stay hourly.
		dailyRows, err := loadAnalysisOverviewDailyStatsWithFilter(db, effective, fullDayStart, fullDayEnd)
		if err != nil {
			return err
		}
		dailyIdentityLookup, err := loadAnalysisProjectionIdentityLookup(db, effective.InstanceID, dailyRows)
		if err != nil {
			return err
		}
		for _, row := range dailyRows {
			accumulator.addStatRow(row, dailyIdentityLookup)
		}
		for _, window := range []struct{ start, end time.Time }{{fullStart, fullDayStart}, {fullDayEnd, fullEnd}} {
			if !window.end.After(window.start) {
				continue
			}
			if err := accumulateAnalysisHourlyWindow(db, effective, accumulator, window.start, window.end); err != nil {
				return err
			}
		}
	}

	accumulator.finalize()
	fillAnalysisFullDayHourlyBuckets(record, filter)
	return nil
}

func accumulateAnalysisHourlyWindow(db *gorm.DB, filter dto.UsageQueryFilter, accumulator *analysisWindowAccumulator, start, end time.Time) error {
	rows, err := loadAnalysisOverviewHourlyStatsWithFilter(db, filter, start, end)
	if err != nil {
		return err
	}
	identityLookup, err := loadAnalysisProjectionIdentityLookup(db, filter.InstanceID, rows)
	if err != nil {
		return err
	}
	for _, row := range rows {
		accumulator.addStatRow(row, identityLookup)
	}
	return nil
}

func loadAnalysisBoundaryEvents(db *gorm.DB, filter dto.UsageQueryFilter, windows []usageOverviewRawEventWindow) ([]entities.UsageEvent, error) {
	events := make([]entities.UsageEvent, 0)
	for _, window := range windows {
		windowEvents, err := loadUsageOverviewEventRangeWithProjection(db, filter, window.start, window.end, window.includeEnd, analysisBoundaryEventProjectionColumns)
		if err != nil {
			return nil, err
		}
		events = append(events, windowEvents...)
	}
	return events, nil
}
