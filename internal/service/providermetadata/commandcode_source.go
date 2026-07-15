package providermetadata

import (
	"context"

	"cpa-usage-keeper/internal/cpa/dto/response"
)

// commandcodeSource 声明 CPA commandcode-api-key endpoint 与 Keeper commandcode provider 的一一边界。
func commandcodeSource() source {
	// CommandCode 是既有必选 endpoint，provider type 使用 CPA 正确拼写。
	return newProviderKeySource("commandcode", "commandcode", "commandcode", "commandcode api keys", false, func(ctx context.Context, fetcher Fetcher) (*response.ProviderKeyConfigResult, error) {
		// 只调用 CommandCode 专属 client 方法。
		return fetcher.FetchCommandCodeAPIKeys(ctx)
	})
}
