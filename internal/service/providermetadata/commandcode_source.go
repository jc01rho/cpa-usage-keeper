package providermetadata

import (
	"context"
	"net/http"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// commandcodeSource 声明 CPA commandcode-api-key endpoint 与 Keeper commandcode provider 的一一边界。
// CommandCode 只由 fork CLIProxyAPIPlus 的 legacy /v0 路由提供；上游 CPA 没有该路由时返回 404，
// 此时视为来源不可用：不产生 warning、不进入 stale 范围，已存 CommandCode 身份保持不变。
func commandcodeSource() source {
	item := newProviderKeySource("commandcode", "commandcode", "commandcode", "commandcode api keys", func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 CommandCode 专属 client 方法。
		return fetcher.FetchCommandCodeAPIKeys(ctx)
	})
	item.fetch = func(ctx context.Context, fetcher Fetcher) sourceResult {
		result, err := fetcher.FetchCommandCodeAPIKeys(ctx)
		if err != nil && result != nil && result.StatusCode == http.StatusNotFound {
			return sourceResult{}
		}
		return fetchProviderKeySource(ctx, fetcher, item, func(context.Context, Fetcher) (*response.ProviderKeyConfigResult, error) {
			return result, err
		})
	}
	return item
}
