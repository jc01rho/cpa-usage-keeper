import React, { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { EmptyState } from '@/components/ui/EmptyState';
import { Select } from '@/components/ui/Select';
import type { UsageEvent, UsageSourceFilterOption } from '@/lib/types';
import {
  calculateCacheRate,
  formatDurationMs,
  formatUsd,
  LATENCY_SOURCE_FIELD,
  normalizeAuthIndex,
} from '@/utils/usage';
import styles from '@/pages/UsagePage.module.scss';

const ALL_FILTER = '__all__';

type SelectOption = { value: string; label: string };

const appendSelectedOption = (
  options: SelectOption[],
  selectedValue: string,
  selectedLabel = selectedValue
) => {
  if (selectedValue === ALL_FILTER || options.some((option) => option.value === selectedValue)) {
    return options;
  }
  return [...options, { value: selectedValue, label: selectedLabel }];
};

type RequestEventRow = {
  id: string;
  timestamp: string;
  timestampMs: number;
  timestampLabel: string;
  apiKey: string;
  model: string;
  reasoningEffort: string;
  requestType: string;
  endpoint: string;
  sourceRaw: string;
  source: string;
  sourceType: string;
  authIndex: string;
  isDelete: boolean;
  failed: boolean;
  latencyMs: number | null;
  ttftMs: number | null;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cachedTokens: number;
  totalTokens: number;
  cacheRate: string;
  cost: number | null;
  costAvailable: boolean;
};

export interface RequestEventsDetailsCardProps {
  events: UsageEvent[];
  loading: boolean;
  page: number;
  pageSize: number;
  pageSizeOptions: readonly number[];
  totalCount: number;
  totalPages: number;
  modelOptions: string[];
  sourceOptions: UsageSourceFilterOption[];
  modelFilter: string;
  sourceFilter: string;
  resultFilter: string;
  onPageChange: (page: number) => void;
  onPageSizeChange: (pageSize: number) => void;
  onModelFilterChange: (model: string) => void;
  onSourceFilterChange: (source: string) => void;
  onResultFilterChange: (result: string) => void;
}

const toNumber = (value: unknown): number => {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return 0;
  return parsed;
};

const formatRequestEventTimestamp = (timestamp: string): string => {
  const match = timestamp.match(/^(\d{4})-(\d{2})-(\d{2})[T\s](\d{2}):(\d{2}):(\d{2})/);
  if (!match) return timestamp || '-';
  return `${match[1]}/${match[2]}/${match[3]} ${match[4]}:${match[5]}:${match[6]}`;
};

const formatCacheRate = (cachedTokens: number, inputTokens: number): string => {
  const rate = calculateCacheRate({ inputTokens, cachedTokens });
  return rate === null ? '-' : `${rate.toFixed(2)}%`;
};

const formatTTFTMs = (ttftMs: number | null): string => {
  if (ttftMs === null || ttftMs <= 0) {
    return '-';
  }
  return formatDurationMs(ttftMs);
};

const parseRequestEndpoint = (rawEndpoint: unknown): { requestType: string; endpoint: string } => {
  const raw = String(rawEndpoint ?? '').trim().replace(/\s+/g, ' ');
  if (!raw) {
    return { requestType: '-', endpoint: '-' };
  }
  const [first, ...rest] = raw.split(' ');
  const upperMethod = first.toUpperCase();
  const hasMethod = ['GET', 'POST'].includes(upperMethod);
  const requestType = upperMethod === 'POST' ? 'SSE' : upperMethod === 'GET' ? 'WS' : '-';
  const path = hasMethod ? rest.join(' ').trim() : raw;
  const normalizedPath = path.startsWith('/v1/') ? path.slice(3) : path === '/v1' ? '/' : path;
  return { requestType, endpoint: normalizedPath || '-' };
};

function RequestEventsTitle({ title, subtitle, eyebrow, totalLabel }: { title: string; subtitle: string; eyebrow: string; totalLabel: string }) {
  return (
    <div className={styles.sectionTitleBlock}>
      <span className={styles.sectionEyebrow}>{eyebrow}</span>
      <div className={styles.requestEventsTitleRow}>
        <h3 className={styles.sectionTitle}>{title}</h3>
        <span className={styles.requestEventsCountBadge}>{totalLabel}</span>
      </div>
      <p className={styles.sectionSubtitle}>{subtitle}</p>
    </div>
  );
}

export function RequestEventsDetailsCard({
  events,
  loading,
  page,
  pageSize,
  pageSizeOptions,
  totalCount,
  totalPages,
  modelOptions: backendModelOptions,
  sourceOptions: backendSourceOptions,
  modelFilter,
  sourceFilter,
  resultFilter,
  onPageChange,
  onPageSizeChange,
  onModelFilterChange,
  onSourceFilterChange,
  onResultFilterChange,
}: RequestEventsDetailsCardProps) {
  const { t } = useTranslation();
  const latencyHint = t('usage_stats.latency_unit_hint', {
    field: LATENCY_SOURCE_FIELD,
    unit: t('usage_stats.duration_unit_ms'),
  });
  const ttftHint = t('usage_stats.ttft_hint');

  const rows = useMemo<RequestEventRow[]>(() => {
    return events.map((event, index) => {
      const timestamp = event.timestamp;
      const timestampMs = Date.parse(timestamp);
      const sourceRaw = String(event.source_raw ?? '').trim() || String(event.source ?? '').trim();
      const authIndexRaw = event.auth_index as unknown;
      const authIndex =
        authIndexRaw === null || authIndexRaw === undefined || authIndexRaw === ''
          ? '-'
          : normalizeAuthIndex(authIndexRaw) || '-';
      const source = String(event.source ?? '').trim() || '-';
      const sourceType = String(event.source_type ?? '').trim();
      const apiKey = String(event.api_key ?? '').trim() || '-';
      const model = String(event.model ?? '').trim() || '-';
      const reasoningEffort = String(event.reasoning_effort ?? '').trim() || '-';
      const endpointFields = parseRequestEndpoint(event.endpoint);
      const inputTokens = Math.max(toNumber(event.tokens?.input_tokens), 0);
      const outputTokens = Math.max(toNumber(event.tokens?.output_tokens), 0);
      const reasoningTokens = Math.max(toNumber(event.tokens?.reasoning_tokens), 0);
      const cachedTokens = Math.max(toNumber(event.tokens?.cached_tokens), 0);
      const totalTokens = Math.max(toNumber(event.tokens?.total_tokens), 0);
      const latencyMs = Number.isFinite(event.latency_ms) ? event.latency_ms : null;
      const ttftMs = Number.isFinite(event.ttft_ms) ? event.ttft_ms as number : null;
      // 费用由后端按当前价格配置运行时计算，前端只负责展示可用/不可用状态。
      const costAvailable = event.cost_available === true;
      const cost = costAvailable ? Math.max(toNumber(event.cost_usd), 0) : null;

      return {
        id: event.id ? String(event.id) : `${timestamp}-${model}-${sourceRaw || source}-${authIndex}-${index}`,
        timestamp,
        timestampMs: Number.isNaN(timestampMs) ? 0 : timestampMs,
        timestampLabel: formatRequestEventTimestamp(timestamp),
        apiKey,
        model,
        reasoningEffort,
        requestType: endpointFields.requestType,
        endpoint: endpointFields.endpoint,
        sourceRaw: sourceRaw || '-',
        source,
        sourceType,
        authIndex,
        isDelete: event.isDelete === true,
        failed: event.failed === true,
        latencyMs,
        ttftMs,
        inputTokens,
        outputTokens,
        reasoningTokens,
        cachedTokens,
        totalTokens,
        cacheRate: formatCacheRate(cachedTokens, inputTokens),
        cost,
        costAvailable,
      };
    });
  }, [events]);

  const hasLatencyData = useMemo(() => rows.some((row) => row.latencyMs !== null), [rows]);

  const modelOptions = useMemo(() => {
    const options = [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...backendModelOptions.map((model) => ({ value: model, label: model })),
    ];
    return appendSelectedOption(options, modelFilter);
  }, [backendModelOptions, modelFilter, t]);

  const sourceOptions = useMemo(() => {
    const options = [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      ...backendSourceOptions.map((source) => ({ value: source.value, label: source.displayName || source.label || source.value })),
    ];
    const selectedSource = backendSourceOptions.find((source) => source.value === sourceFilter);
    const selectedLabel = selectedSource?.displayName || selectedSource?.label;
    return appendSelectedOption(options, sourceFilter, selectedLabel || sourceFilter);
  }, [backendSourceOptions, sourceFilter, t]);

  const resultOptions = useMemo(
    () => [
      { value: ALL_FILTER, label: t('usage_stats.filter_all') },
      { value: 'success', label: t('usage_stats.success') },
      { value: 'failed', label: t('usage_stats.failure') },
    ],
    [t]
  );

  const modelOptionSet = useMemo(
    () => new Set(modelOptions.map((option) => option.value)),
    [modelOptions]
  );
  const sourceOptionSet = useMemo(
    () => new Set(sourceOptions.map((option) => option.value)),
    [sourceOptions]
  );
  const resultOptionSet = useMemo(
    () => new Set(resultOptions.map((option) => option.value)),
    [resultOptions]
  );

  const effectiveModelFilter = modelOptionSet.has(modelFilter) ? modelFilter : ALL_FILTER;
  const effectiveSourceFilter = sourceOptionSet.has(sourceFilter) ? sourceFilter : ALL_FILTER;
  const effectiveResultFilter = resultOptionSet.has(resultFilter) ? resultFilter : ALL_FILTER;

  const hasActiveFilters =
    modelFilter !== ALL_FILTER ||
    sourceFilter !== ALL_FILTER ||
    resultFilter !== ALL_FILTER;

  const computedTotalPages = pageSize > 0 ? Math.ceil(totalCount / pageSize) : 0;
  const safeTotalPages = Math.max(totalPages, computedTotalPages, rows.length > 0 ? 1 : 0);
  const safePage = safeTotalPages > 0 ? Math.min(Math.max(page, 1), safeTotalPages) : 0;
  const pageLabel = safeTotalPages > 0 ? `${safePage} / ${safeTotalPages}` : t('usage_stats.request_events_page_empty');

  const handleClearFilters = () => {
    onModelFilterChange(ALL_FILTER);
    onSourceFilterChange(ALL_FILTER);
    onResultFilterChange(ALL_FILTER);
  };

  return (
    <Card
      className={styles.requestEventsCard}
      title={
        <RequestEventsTitle
          eyebrow={t('usage_stats.request_events_eyebrow')}
          title={t('usage_stats.request_events_title')}
          subtitle={t('usage_stats.request_events_subtitle')}
          totalLabel={t('usage_stats.request_events_total_count', { count: totalCount })}
        />
      }
      extra={
        <div className={styles.requestEventsActions}>
          <Button
            variant="ghost"
            size="sm"
            className={styles.usagePillAction}
            onClick={handleClearFilters}
            disabled={!hasActiveFilters}
          >
            {t('usage_stats.clear_filters')}
          </Button>
        </div>
      }
    >
      <div className={styles.requestEventsToolbar}>
        <div className={styles.requestEventsFiltersGroup}>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_model')}
            </span>
            <Select
              value={effectiveModelFilter}
              options={modelOptions}
              onChange={onModelFilterChange}
              className={`${styles.requestEventsSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_model')}
              fullWidth={false}
            />
          </label>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_source')}
            </span>
            <Select
              value={effectiveSourceFilter}
              options={sourceOptions}
              onChange={onSourceFilterChange}
              className={`${styles.requestEventsSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_source')}
              fullWidth={false}
            />
          </label>
          <label className={styles.requestEventsFilterItem}>
            <span className={styles.requestEventsFilterLabel}>
              {t('usage_stats.request_events_filter_result')}
            </span>
            <Select
              value={effectiveResultFilter}
              options={resultOptions}
              onChange={onResultFilterChange}
              className={`${styles.requestEventsResultSelect} ${styles.usagePillControl}`}
              ariaLabel={t('usage_stats.request_events_filter_result')}
              fullWidth={false}
            />
          </label>
        </div>
      </div>

      {loading && rows.length === 0 ? (
        <div className={styles.hint}>{t('common.loading')}</div>
      ) : rows.length === 0 ? (
        <EmptyState
          title={t('usage_stats.request_events_empty_title')}
          description={t('usage_stats.request_events_empty_desc')}
        />
      ) : (
        <>
          <div className={styles.requestEventsTableWrapper}>
            <table className={styles.table}>
              <thead>
                <tr>
                  <th>{t('usage_stats.request_events_timestamp')}</th>
                  <th>{t('usage_stats.api_key_filter')}</th>
                  <th>{t('usage_stats.request_events_source')}</th>
                  <th>{t('usage_stats.model_name')}</th>
                  <th title={t('usage_stats.reasoning_effort_hint')}>{t('usage_stats.reasoning_effort')}</th>
                  <th>{t('usage_stats.request_events_result')}</th>
                  <th title={ttftHint}>{t('usage_stats.ttft')}</th>
                  {hasLatencyData && <th title={latencyHint}>{t('usage_stats.latency')}</th>}
                  <th>{t('usage_stats.request_type')}</th>
                  <th>{t('usage_stats.request_endpoint')}</th>
                  <th>{t('usage_stats.input_tokens')}</th>
                  <th>{t('usage_stats.output_tokens')}</th>
                  <th className={styles.requestEventsReasoningHeader}>{t('usage_stats.reasoning_tokens')}</th>
                  <th>{t('usage_stats.cached_tokens')}</th>
                  <th>{t('usage_stats.cache_rate')}</th>
                  <th>{t('usage_stats.total_tokens')}</th>
                  <th>{t('usage_stats.total_cost')}</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.id}>
                    <td title={row.timestamp} className={styles.requestEventsTimestamp}>
                      {row.timestampLabel}
                    </td>
                    <td className={styles.requestEventsAPIKeyCell} title={row.apiKey}>{row.apiKey}</td>
                    <td className={styles.requestEventsSourceCell} title={row.source}>
                      <span className={styles.requestEventsSourceStack}>
                        <span className={styles.requestEventsSourceValue}>{row.source}</span>
                        {(row.isDelete || row.sourceType) && (
                          <span className={styles.requestEventsSourceTags}>
                            {row.sourceType && (
                              <span className={styles.credentialType}>{row.sourceType}</span>
                            )}
                            {row.isDelete && (
                              <span className={styles.requestEventsDeletedTag}>{t('usage_stats.deleted')}</span>
                            )}
                          </span>
                        )}
                      </span>
                    </td>
                    <td className={styles.modelCell}>{row.model}</td>
                    <td>{row.reasoningEffort}</td>
                    <td>
                      <span
                        className={
                          row.failed
                            ? styles.requestEventsResultFailed
                            : styles.requestEventsResultSuccess
                        }
                      >
                        {row.failed ? t('usage_stats.failure') : t('usage_stats.success')}
                      </span>
                    </td>
                    <td className={styles.durationCell}>{formatTTFTMs(row.ttftMs)}</td>
                    {hasLatencyData && (
                      <td className={styles.durationCell}>{formatDurationMs(row.latencyMs)}</td>
                    )}
                    <td>{row.requestType}</td>
                    <td className={styles.requestEventsEndpointCell} title={row.endpoint}>{row.endpoint}</td>
                    <td>{row.inputTokens.toLocaleString()}</td>
                    <td>{row.outputTokens.toLocaleString()}</td>
                    <td>{row.reasoningTokens.toLocaleString()}</td>
                    <td>{row.cachedTokens.toLocaleString()}</td>
                    <td>{row.cacheRate}</td>
                    <td>{row.totalTokens.toLocaleString()}</td>
                    <td title={row.costAvailable ? undefined : t('usage_stats.cost_need_price')}>
                      {row.costAvailable && row.cost !== null ? formatUsd(row.cost) : '-'}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className={styles.requestEventsPaginationFooter}>
            <div className={styles.requestEventsPaginationControls}>
              <label className={styles.requestEventsPageSizeControl}>
                <span>{t('usage_stats.request_events_rows_per_page')}</span>
                <select value={pageSize} onChange={(event) => onPageSizeChange(Number(event.target.value))} disabled={loading}>
                  {pageSizeOptions.map((option) => <option key={option} value={option}>{option}</option>)}
                </select>
              </label>
              <button type="button" className={styles.requestEventsPagerButton} onClick={() => onPageChange(page - 1)} disabled={loading || safePage <= 1}>
                {t('usage_stats.request_events_previous_page')}
              </button>
              <span className={styles.requestEventsPaginationPage}>{pageLabel}</span>
              <button type="button" className={styles.requestEventsPagerButton} onClick={() => onPageChange(page + 1)} disabled={loading || safeTotalPages === 0 || safePage >= safeTotalPages}>
                {t('usage_stats.request_events_next_page')}
              </button>
            </div>
          </div>
        </>
      )}
    </Card>
  );
}
