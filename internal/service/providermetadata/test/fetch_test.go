package providermetadata_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestFetchFoldsReverseCompletionInRegistryOrder(t *testing.T) {
	fetcher := newGatedProviderFetcher(t)
	resultCh := startProviderFetch(context.Background(), fetcher)

	waitForSources(t, fetcher.entered, registrySourceOrder)
	// reverseOrder 与 registry 完全相反。
	reverseOrder := []string{"openai", "meta", "commandcode", "vertex", "claude", "gemini-interactions", "gemini", "xai", "codex"}
	// 逐个释放并确认完成，确保真实完成顺序可控。
	for _, source := range reverseOrder {
		fetcher.release(source)
		waitForSources(t, fetcher.done, []string{source})
	}
	outcome := waitForFetchOutcome(t, resultCh)
	if outcome.err != nil {
		t.Fatalf("Fetch returned error: %v", outcome.err)
	}
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, registrySourceOrder) {
		t.Fatalf("FetchedProviderTypes = %#v", outcome.snapshot.FetchedProviderTypes)
	}
	gotAuthIndexes := make([]string, 0, len(outcome.snapshot.Credentials))
	for _, credential := range outcome.snapshot.Credentials {
		gotAuthIndexes = append(gotAuthIndexes, credential.AuthIndex)
	}
	// wantAuthIndexes 是 registry/source entry 的稳定顺序。
	wantAuthIndexes := []string{"codex-auth", "xai-auth", "gemini-auth", "gemini-interactions-auth", "claude-auth", "vertex-auth", "commandcode-auth", "meta-auth", "openai-auth"}
	// 完成顺序不能改变 Credential 顺序。
	if !reflect.DeepEqual(gotAuthIndexes, wantAuthIndexes) {
		t.Fatalf("auth indexes = %#v, want %#v", gotAuthIndexes, wantAuthIndexes)
	}
}

func TestFetchKeepsOtherSourcesWhenOneProviderFails(t *testing.T) {
	fetcher := newGatedProviderFetcher(t)
	fetcher.errors["gemini"] = errors.New("gemini unavailable")
	resultCh := startProviderFetch(context.Background(), fetcher)

	// 七个 endpoint 必须在任一结果返回前全部进入。
	waitForSources(t, fetcher.entered, registrySourceOrder)
	fetcher.releaseAll()
	waitForSources(t, fetcher.done, registrySourceOrder)
	outcome := waitForFetchOutcome(t, resultCh)
	if outcome.err == nil || outcome.err.Error() != "fetch gemini api keys: gemini unavailable" {
		t.Fatalf("error = %v", outcome.err)
	}
	// 失败 Gemini 不进入 fetched types，其余七来源保持 registry 相对顺序。
	wantTypes := []string{"codex", "xai", "gemini-interactions", "claude", "vertex", "commandcode", "meta", "openai"}
	// 实际 fetched types 必须与成功来源一致。
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, wantTypes) {
		t.Fatalf("FetchedProviderTypes = %#v, want %#v", outcome.snapshot.FetchedProviderTypes, wantTypes)
	}
	// 其它来源的 Credential 必须全部保留。
	if len(outcome.snapshot.Credentials) != 8 {
		t.Fatalf("Credentials = %#v", outcome.snapshot.Credentials)
	}
}

func TestFetchPreservesCompletedSourcesAndWaitsForCancellation(t *testing.T) {
	fetcher := newGatedProviderFetcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resultCh := startProviderFetch(ctx, fetcher)

	waitForSources(t, fetcher.entered, registrySourceOrder)
	fetcher.release("codex")
	// 等待 Codex 报告完成，固定部分成功边界。
	waitForSources(t, fetcher.done, []string{"codex"})
	cancel()
	// 剩余八个 endpoint 必须全部退出，不允许 goroutine 泄漏。
	waitForSources(t, fetcher.done, []string{"xai", "gemini", "gemini-interactions", "claude", "vertex", "commandcode", "meta", "openai"})
	// 读取 context 取消后的部分成功结果。
	outcome := waitForFetchOutcome(t, resultCh)
	if !reflect.DeepEqual(outcome.snapshot.FetchedProviderTypes, []string{"codex"}) || len(outcome.snapshot.Credentials) != 1 || outcome.snapshot.Credentials[0].AuthIndex != "codex-auth" {
		t.Fatalf("snapshot = %#v", outcome.snapshot)
	}
	// 取消 warning 必须按剩余来源的 registry 顺序稳定归并。
	wantError := "fetch xai api keys: context canceled; fetch gemini api keys: context canceled; fetch interactions api keys: context canceled; fetch claude api keys: context canceled; fetch vertex api keys: context canceled; fetch commandcode api keys: context canceled; fetch meta api keys: context canceled; fetch openai compatibility: context canceled"
	// 实际 error 必须完整包含所有剩余来源且顺序稳定。
	if outcome.err == nil || outcome.err.Error() != wantError {
		t.Fatalf("error = %v, want %q", outcome.err, wantError)
	}
}
