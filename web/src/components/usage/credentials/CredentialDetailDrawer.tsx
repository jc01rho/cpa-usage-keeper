import { useCallback, useEffect, useId, useLayoutEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Modal } from '@/components/ui/Modal'
import { IconRefreshCw } from '@/components/ui/icons'
import { ProviderBrandIcon } from '@/components/ProviderBrandIcon'
import { RequestEventLogModal } from '@/components/usage/RequestEventLogModal'
import { ApiError, fetchUsageEvents } from '@/lib/api'
import type { UsageEvent, UsageEventRequestLogResponse } from '@/lib/types'
import { AuthFileQuotaPanel } from './AuthFileCredentialsSection'
import { CredentialHealthPanel } from './CredentialHealthPanel'
import { CredentialPriorityBadge, formatCredentialNumber, formatCredentialPercent } from './CredentialSectionShell'
import { CredentialSubscriptionBadge } from './CredentialSubscriptionBadge'
import { CredentialRequestEventsList } from './CredentialRequestEventsList'
import type { CredentialDetailSelection } from './credentialViewModels'
import styles from './CredentialDetailDrawer.module.scss'
import credentialStyles from './CredentialSections.module.scss'

const REQUEST_EVENTS_PAGE_SIZE = 50

type CredentialDetailTab = 'overview' | 'requests'

interface CredentialDetailDrawerProps {
  open: boolean
  selection: CredentialDetailSelection | null
  onAuthRequired?: () => void
  requestLogAccessEnabled?: boolean
  onRequestLogOpen?: (event: UsageEvent) => void
  requestLogLoadingEventId?: string | null
  requestLogResponse?: UsageEventRequestLogResponse | null
  requestLogError?: string
  onRequestLogClose?: () => void
  onRequestLogDownload?: (eventId: string) => void
  requestLogDownloading?: boolean
  onClose: () => void
}

export function appendCredentialDetailEvents(
  currentEvents: readonly UsageEvent[],
  incomingEvents: readonly UsageEvent[],
): UsageEvent[] {
  const seen = new Set(currentEvents.map((event) => String(event.id ?? '').trim()).filter(Boolean))
  const merged = [...currentEvents]
  for (const event of incomingEvents) {
    const id = String(event.id ?? '').trim()
    if (id && seen.has(id)) continue
    if (id) seen.add(id)
    merged.push(event)
  }
  return merged
}

export function CredentialDetailDrawer({
  open,
  selection,
  onAuthRequired,
  requestLogAccessEnabled = false,
  onRequestLogOpen,
  requestLogLoadingEventId = null,
  requestLogResponse = null,
  requestLogError = '',
  onRequestLogClose,
  onRequestLogDownload,
  requestLogDownloading = false,
  onClose,
}: CredentialDetailDrawerProps) {
  const { t } = useTranslation()
  const overviewTabId = useId()
  const requestsTabId = useId()
  const overviewPanelId = useId()
  const requestsPanelId = useId()
  const overviewTabRef = useRef<HTMLButtonElement | null>(null)
  const requestsTabRef = useRef<HTMLButtonElement | null>(null)
  const [activeTab, setActiveTab] = useState<CredentialDetailTab>('overview')
  const [events, setEvents] = useState<UsageEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsLoadingMore, setEventsLoadingMore] = useState(false)
  const [eventsAutoLoadMore, setEventsAutoLoadMore] = useState(true)
  const [eventsError, setEventsError] = useState('')
  const [eventsNextCursor, setEventsNextCursor] = useState<string | null>(null)
  const firstPageControllerRef = useRef<AbortController | null>(null)
  const loadMoreControllerRef = useRef<AbortController | null>(null)

  const row = selection?.row ?? null
  const identity = row?.identity ?? null
  const selectionKey = selection ? `${selection.kind}:${identity?.id || identity?.identity || ''}` : ''
  const sourceFilter = identity?.identity?.trim() ?? ''
  const authTypeFilter = identity?.auth_type

  const resetRequestEvents = useCallback(() => {
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
    firstPageControllerRef.current = null
    loadMoreControllerRef.current = null
    setEvents([])
    setEventsLoading(false)
    setEventsLoadingMore(false)
    setEventsAutoLoadMore(true)
    setEventsError('')
    setEventsNextCursor(null)
  }, [])

  // 关闭动画继续使用 Modal 的内容快照；同步清理内部状态，保证下次打开不会提交上一凭证的数据。
  useLayoutEffect(() => {
    if (open) return
    setActiveTab('overview')
    resetRequestEvents()
  }, [open, resetRequestEvents])

  useEffect(() => {
    if (!open || !selectionKey) return
    setActiveTab('overview')
    resetRequestEvents()
  }, [open, resetRequestEvents, selectionKey])

  const loadFirstPage = useCallback(async () => {
    if (!open || activeTab !== 'requests' || !sourceFilter) return
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
    loadMoreControllerRef.current = null
    const controller = new AbortController()
    firstPageControllerRef.current = controller
    setEventsLoading(true)
    setEventsLoadingMore(false)
    setEventsAutoLoadMore(true)
    setEventsError('')
    try {
      // 详情列表固定从当前凭证最近的原始事件开始，不继承外层页面的查询条件。
      const response = await fetchUsageEvents(undefined, controller.signal, {
        authType: authTypeFilter,
        pageSize: REQUEST_EVENTS_PAGE_SIZE,
        cursorMode: true,
        source: sourceFilter,
      })
      if (firstPageControllerRef.current !== controller) return
      setEvents(response.events)
      setEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      setEvents([])
      setEventsNextCursor(null)
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      setEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_requests_load_failed'))
    } finally {
      if (firstPageControllerRef.current === controller) {
        firstPageControllerRef.current = null
        setEventsLoading(false)
      }
    }
  }, [activeTab, authTypeFilter, onAuthRequired, open, sourceFilter, t])

  useEffect(() => {
    if (!open || activeTab !== 'requests') return
    void loadFirstPage()
    return () => {
      firstPageControllerRef.current?.abort()
      firstPageControllerRef.current = null
      loadMoreControllerRef.current?.abort()
      loadMoreControllerRef.current = null
    }
  }, [activeTab, loadFirstPage, open])

  useEffect(() => () => {
    firstPageControllerRef.current?.abort()
    loadMoreControllerRef.current?.abort()
  }, [])

  const activateTab = useCallback((tab: CredentialDetailTab, focus = false) => {
    if (tab === 'overview') onRequestLogClose?.()
    setActiveTab(tab)
    if (focus) {
      const target = tab === 'overview' ? overviewTabRef.current : requestsTabRef.current
      target?.focus()
    }
  }, [onRequestLogClose])

  const handleTabKeyDown = useCallback((event: ReactKeyboardEvent<HTMLButtonElement>) => {
    const currentTab: CredentialDetailTab = event.currentTarget === requestsTabRef.current ? 'requests' : 'overview'
    let nextTab: CredentialDetailTab | null = null
    switch (event.key) {
      case 'ArrowLeft':
      case 'ArrowRight':
        nextTab = currentTab === 'overview' ? 'requests' : 'overview'
        break
      case 'Home':
        nextTab = 'overview'
        break
      case 'End':
        nextTab = 'requests'
        break
      default:
        return
    }
    event.preventDefault()
    activateTab(nextTab, true)
  }, [activateTab])

  const loadMore = useCallback(async () => {
    const cursor = eventsNextCursor?.trim()
    if (!cursor || loadMoreControllerRef.current || eventsLoading || eventsLoadingMore || !sourceFilter) return
    const controller = new AbortController()
    loadMoreControllerRef.current = controller
    setEventsLoadingMore(true)
    setEventsError('')
    try {
      const response = await fetchUsageEvents(undefined, controller.signal, {
        authType: authTypeFilter,
        pageSize: REQUEST_EVENTS_PAGE_SIZE,
        cursorMode: true,
        cursor,
        source: sourceFilter,
      })
      if (loadMoreControllerRef.current !== controller) return
      setEvents((current) => appendCredentialDetailEvents(current, response.events))
      setEventsAutoLoadMore(true)
      setEventsNextCursor(response.has_more === true ? response.next_cursor?.trim() || null : null)
    } catch (error) {
      if (controller.signal.aborted) return
      if (error instanceof ApiError && error.status === 401) {
        onAuthRequired?.()
        return
      }
      setEventsAutoLoadMore(false)
      setEventsError(error instanceof Error ? error.message : t('usage_stats.credentials_detail_requests_load_failed'))
    } finally {
      if (loadMoreControllerRef.current === controller) {
        loadMoreControllerRef.current = null
        setEventsLoadingMore(false)
      }
    }
  }, [authTypeFilter, eventsLoading, eventsLoadingMore, eventsNextCursor, onAuthRequired, sourceFilter, t])

  if (!selection || !row || !identity) return null

  const title = (
    <div className={styles.drawerTitle}>
      <ProviderBrandIcon providerType={identity.type} size={36} ariaLabel={row.typeLabel} />
      <div className={styles.drawerTitleText}>
        <strong>{row.displayName}</strong>
        <span data-credential-detail-subtitle>
          {selection.kind === 'auth-file'
            ? identity.file_name?.trim() || '-'
            : `${row.providerLabel} · ${row.typeLabel} · ${row.authTypeLabel}`}
        </span>
      </div>
      <div className={styles.drawerTitleBadges}>
        {selection.kind === 'auth-file' && selection.row.subscriptionBadge
          ? <CredentialSubscriptionBadge model={selection.row.subscriptionBadge} />
          : null}
        {row.priorityLabel ? <CredentialPriorityBadge>{row.priorityLabel}</CredentialPriorityBadge> : null}
        <span className={identity.disabled || identity.is_deleted ? styles.statusDisabled : styles.statusEnabled}>
          {identity.is_deleted
            ? t('usage_stats.deleted')
            : identity.disabled
              ? t('usage_stats.credentials_detail_disabled')
              : t('usage_stats.credentials_detail_enabled')}
        </span>
      </div>
    </div>
  )

  return (
    <>
      <Modal open={open} title={title} variant="drawer" width={920} className={styles.drawer} onClose={onClose}>
        <div className={styles.tabBar} data-credential-detail-tab-bar>
          <div className={styles.tabs} role="tablist" aria-label={t('usage_stats.credentials_detail_tabs')}>
            <button
              ref={overviewTabRef}
              id={overviewTabId}
              type="button"
              role="tab"
              className={styles.tabButton}
              aria-selected={activeTab === 'overview'}
              aria-controls={overviewPanelId}
              tabIndex={activeTab === 'overview' ? 0 : -1}
              data-credential-detail-tab="overview"
              onClick={() => activateTab('overview')}
              onKeyDown={handleTabKeyDown}
            >
              {t('usage_stats.credentials_detail_overview_tab')}
            </button>
            <button
              ref={requestsTabRef}
              id={requestsTabId}
              type="button"
              role="tab"
              className={styles.tabButton}
              aria-selected={activeTab === 'requests'}
              aria-controls={requestsPanelId}
              tabIndex={activeTab === 'requests' ? 0 : -1}
              data-credential-detail-tab="requests"
              onClick={() => activateTab('requests')}
              onKeyDown={handleTabKeyDown}
            >
              {t('usage_stats.credentials_detail_requests_tab')}
            </button>
          </div>
          {activeTab === 'requests' && eventsError && events.length === 0 && !eventsLoading ? (
            <button
              type="button"
              className={`${credentialStyles.credentialRowRefreshButton} ${styles.retryButton}`}
              data-credential-detail-retry
              aria-label={t('common.retry')}
              onClick={() => void loadFirstPage()}
            >
              <IconRefreshCw size={13} />
            </button>
          ) : null}
        </div>

        {activeTab === 'overview' ? (
          <section id={overviewPanelId} role="tabpanel" aria-labelledby={overviewTabId} className={styles.overviewPanel}>
          <div className={styles.summaryGrid}>
            <DetailMetric label={t('usage_stats.total_requests')} value={formatCredentialNumber(row.totalRequests)} detail={`${t('usage_stats.success')} ${formatCredentialNumber(row.successCount)} · ${t('usage_stats.failure')} ${formatCredentialNumber(row.failureCount)}`} />
            <DetailMetric label={t('usage_stats.success_rate')} value={formatCredentialPercent(row.successRate)} />
            <DetailMetric label={t('usage_stats.total_tokens')} value={formatCredentialNumber(row.totalTokens)} />
            <DetailMetric label={t('usage_stats.cache_rate')} value={formatCredentialPercent(row.cacheReadRate)} />
          </div>
          <div className={`${styles.overviewGrid} ${selection.kind === 'ai-provider' ? styles.overviewGridSingle : ''}`.trim()}>
            <section className={styles.overviewSection}>
              <h3>{t('usage_stats.credentials_detail_identity')}</h3>
              <dl className={styles.identityList}>
                <dt>{t('usage_stats.credentials_detail_provider')}</dt><dd>{row.providerLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_type')}</dt><dd>{row.typeLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_auth_type')}</dt><dd>{row.authTypeLabel || '-'}</dd>
                <dt>{t('usage_stats.credentials_detail_priority')}</dt><dd>{row.priorityLabel || '-'}</dd>
              </dl>
            </section>
            {selection.kind === 'auth-file' ? (
              <section className={styles.overviewSection}>
                <h3>{t('usage_stats.credentials_detail_quota')}</h3>
                <AuthFileQuotaPanel row={selection.row} quotaUsageMode="current" />
              </section>
            ) : null}
          </div>
          <section className={`${styles.overviewSection} ${styles.healthSection}`.trim()}>
            <h3>{t('usage_stats.credentials_detail_health')}</h3>
            <CredentialHealthPanel
              displayName={row.displayName}
              health={row.credentialHealth}
              lastUsedAt={identity.last_used_at}
              statsUpdatedAt={identity.stats_updated_at}
            />
          </section>
          </section>
        ) : (
          <section id={requestsPanelId} role="tabpanel" aria-labelledby={requestsTabId} className={styles.requestsPanel}>
            {eventsError ? <div className={styles.requestError} role="status">{eventsError}</div> : null}
            {eventsError && events.length === 0 ? null : (
              <CredentialRequestEventsList
                events={events}
                loading={eventsLoading}
                hasMore={Boolean(eventsNextCursor)}
                loadingMore={eventsLoadingMore}
                autoLoadMore={eventsAutoLoadMore}
                onLoadMore={() => void loadMore()}
                requestLogAccessEnabled={requestLogAccessEnabled}
                onRequestLogOpen={onRequestLogOpen}
                requestLogLoadingEventId={requestLogLoadingEventId}
              />
            )}
          </section>
        )}
      </Modal>
      {open ? (
        <RequestEventLogModal
          loadingEventId={requestLogLoadingEventId}
          response={requestLogResponse}
          error={requestLogError}
          onClose={onRequestLogClose}
          onDownload={onRequestLogDownload}
          downloading={requestLogDownloading}
        />
      ) : null}
    </>
  )
}

function DetailMetric({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div className={styles.summaryMetric}>
      <span>{label}</span>
      <strong>{value}</strong>
      {detail ? <small>{detail}</small> : null}
    </div>
  )
}
