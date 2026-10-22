package service

import (
	"context"
	"fmt"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service/providermetadata"
	"cpa-usage-keeper/internal/timeutil"
	"github.com/sirupsen/logrus"
)

// SyncMetadata 同步三类 CPA metadata，并只在三类数据库写入都成功后通知后台 identity 聚合。
func (s *SyncService) SyncMetadata(ctx context.Context) error {
	// metadata 入口仍复用原 validate，并保留老构造器的 client 回填行为。
	if err := s.validate(syncMetadataRequired); err != nil {
		// 配置不完整时不发起任何远端读取。
		return err
	}
	// 开始日志保持原有 debug 级别和 message。
	logrus.Debug("metadata sync started")
	// 同一轮 metadata 写入与兼容同步补算共用该时间；后台 Runner 使用自己的事务时间。
	fetchedAt := timeutil.NormalizeStorageTime(s.now())
	// Credentials 先读取，但失败不跳过后续两类 metadata。
	credentialsResult, credentialsErr := s.metadataFetcher.FetchCredentials(ctx)
	// 管理 API Keys 保持第二个读取位置，失败同样不阻止 provider。
	apiKeysResult, apiKeysErr := s.metadataFetcher.FetchManagementAPIKeys(ctx)
	// 八个 provider endpoint 只在纯包内并发，返回按 registry 确定性归并的 snapshot。
	// provider 配置变化极少，按独立 TTL 节流，避免每次 refresh 通知都把八个端点重新打一遍。
	providerSnapshot, providerFetchErr, providerSkipped := s.fetchProviderMetadataThrottled(ctx, fetchedAt)
	// Credentials 先进入自己的 repository 事务，保持原有写入顺序。
	authSyncErr := syncCredentials(ctx, s.db, credentialsResult, credentialsErr, fetchedAt)
	// 管理 API Keys 第二个串行写入，不与 SQLite provider 写入并发。
	apiKeySyncErr := syncManagementAPIKeys(s.db, apiKeysResult, apiKeysErr, fetchedAt)
	// provider snapshot 最后一次进入 scoped replace 单事务，并分开返回 persistence error 与 fetch warning。
	// 节流跳过的轮次必须完全跳过 replace，否则会把已有 provider 行当成空快照清掉。
	var providerSyncErr, providerWarningErr error
	if !providerSkipped {
		providerSyncErr, providerWarningErr = persistProviderMetadata(ctx, s.db, providerSnapshot, providerFetchErr, fetchedAt)
	}
	// 三类持久化错误按 Credentials、管理 key、provider 的既有顺序合并。
	upsertErr := joinErrors(authSyncErr, apiKeySyncErr, providerSyncErr)
	// aggregateErr 只承接没有 notifier 的兼容同步补算错误。
	var aggregateErr error
	// 任一数据库写入失败都阻止基于半成品 identity 发送通知或执行兼容补算。
	if upsertErr == nil {
		// provider fetch warning 不属于持久化失败，因此仍允许已提交 identity 唤醒后台 runner。
		if s.usageAggregation != nil {
			// notifier 只写内存并发 wake，不在 metadata 热路径执行 SQLite 聚合事务。
			s.usageAggregation.NotifyUsageIdentitiesChanged()
		} else {
			// 没有 notifier 的既有构造路径继续在返回前补算历史 identity 事件。
			aggregateErr = repository.AggregateUsageIdentityStatsForInstance(ctx, s.db, entities.LegacyCPAInstanceID, fetchedAt)
			// 错误包装保持 main 对兼容调用方的稳定文本。
			if aggregateErr != nil {
				aggregateErr = fmt.Errorf("aggregate usage identity stats: %w", aggregateErr)
			}
		}
	}
	// 最终顺序保持 persistence、兼容 aggregate、provider warning；生产后台聚合仍有独立生命周期。
	err := joinErrors(upsertErr, aggregateErr, providerWarningErr)
	// 完成日志默认沿用 completed 状态。
	fields := logrus.Fields{
		// status 字段保持现有 dashboard/runtime 观察语义。
		"status": "completed",
	}
	// 任一 warning 或错误都只改变完成状态，不回滚已提交的独立事务。
	if err != nil {
		// completed_with_warnings 保持原有非 fail-fast 表达。
		fields["status"] = "completed_with_warnings"
		// error 只写稳定合并后的错误；provider client 已保证不泄漏 response body。
		fields["error"] = err.Error()
	}
	// 完成日志保持原有 debug 级别和 message。
	logrus.WithFields(fields).Debug("metadata sync finished")
	// 返回全部可观察错误，同时保留本轮已提交数据。
	return err
}

// fetchProviderMetadataThrottled 在 TTL 内跳过 provider 配置拉取，
// 返回 skipped=true 时调用方必须保留已有 provider 行而不是写入空快照。
func (s *SyncService) fetchProviderMetadataThrottled(ctx context.Context, now time.Time) (providermetadata.Snapshot, error, bool) {
	// 零值或非正间隔表示不做节流，保持旧行为。
	if s.providerMetadataRefreshInterval <= 0 {
		return fetchProviderMetadata(ctx, s.metadataFetcher)
	}
	// 读取最近一次成功拉取时间；失败轮次不推进时间戳，下一轮会立即重试。
	s.providerMetadataMu.Lock()
	lastFetchedAt := s.providerMetadataFetchedAt
	s.providerMetadataMu.Unlock()
	if !lastFetchedAt.IsZero() && now.Sub(lastFetchedAt) < s.providerMetadataRefreshInterval {
		return providermetadata.Snapshot{}, nil, true
	}
	snapshot, err, _ := fetchProviderMetadata(ctx, s.metadataFetcher)
	if err == nil {
		// 只有完整成功的一轮才推进时间戳，避免部分失败的 provider 被节流锁定。
		s.providerMetadataMu.Lock()
		s.providerMetadataFetchedAt = now
		s.providerMetadataMu.Unlock()
	}
	return snapshot, err, false
}

func fetchProviderMetadata(ctx context.Context, fetcher providermetadata.Fetcher) (providermetadata.Snapshot, error, bool) {
	// 八个 provider endpoint 只在纯包内并发，返回按 registry 确定性归并的 snapshot。
	snapshot, err := providermetadata.Fetch(ctx, fetcher)
	return snapshot, err, false
}
