package providermetadata_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"cpa-usage-keeper/internal/cpa/dto/providerconfig"
	"cpa-usage-keeper/internal/cpa/dto/response"
	"cpa-usage-keeper/internal/service/providermetadata"
)

type providerFetcherStub struct {
	codexResult        *response.ProviderKeyConfigResult
	codexErr           error
	xaiResult          *response.ProviderKeyConfigResult
	xaiErr             error
	geminiResult       *response.ProviderKeyConfigResult
	geminiErr          error
	interactionsResult *response.ProviderKeyConfigResult
	// interactionsErr 保存 Gemini Interactions endpoint error。
	interactionsErr error
	// claudeResult 保存 Claude endpoint result。
	claudeResult *response.ProviderKeyConfigResult
	// claudeErr 保存 Claude endpoint error。
	claudeErr error
	// vertexResult 保存 Vertex endpoint result。
	vertexResult *response.ProviderKeyConfigResult
	// vertexErr 保存 Vertex endpoint error。
	vertexErr error
	// commandcodeResult 保存 CommandCode endpoint result。
	commandcodeResult *response.ProviderKeyConfigResult
	// commandcodeErr 保存 CommandCode endpoint error。
	commandcodeErr error
	// metaResult 保存 Meta endpoint result。
	metaResult *response.ProviderKeyConfigResult
	// metaErr 保存 Meta endpoint error。
	metaErr error
	// openAIResult 保存 OpenAI Compatibility endpoint result。
	openAIResult *response.OpenAICompatibilityResult
	// openAIErr 保存 OpenAI Compatibility endpoint error。
	openAIErr error
}

func (s *providerFetcherStub) FetchCodexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.codexResult, s.codexErr
}

func (s *providerFetcherStub) FetchXAIAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.xaiResult, s.xaiErr
}

func (s *providerFetcherStub) FetchGeminiAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.geminiResult, s.geminiErr
}

func (s *providerFetcherStub) FetchInteractionsAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.interactionsResult, s.interactionsErr
}

func (s *providerFetcherStub) FetchClaudeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.claudeResult, s.claudeErr
}

func (s *providerFetcherStub) FetchVertexAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.vertexResult, s.vertexErr
}

// FetchCommandCodeAPIKeys 返回测试配置的 CommandCode 结果。
func (s *providerFetcherStub) FetchCommandCodeAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	// endpoint 间不共享状态，便于验证 optional 404。
	return s.commandcodeResult, s.commandcodeErr
}

func (s *providerFetcherStub) FetchMetaAPIKeys(context.Context) (*response.ProviderKeyConfigResult, error) {
	return s.metaResult, s.metaErr
}

// FetchOpenAICompatibility 返回测试配置的 OpenAI Compatibility 结果。
func (s *providerFetcherStub) FetchOpenAICompatibility(context.Context) (*response.OpenAICompatibilityResult, error) {
	return s.openAIResult, s.openAIErr
}

func successfulProviderFetcher() *providerFetcherStub {
	return &providerFetcherStub{
		codexResult:        &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		xaiResult:          &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		geminiResult:       &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		interactionsResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Claude 默认成功空列表。
		claudeResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Vertex 默认成功空列表。
		vertexResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// CommandCode 默认成功空列表。
		commandcodeResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// Meta 默认成功空列表。
		metaResult: &response.ProviderKeyConfigResult{StatusCode: http.StatusOK},
		// OpenAI 默认成功空列表。
		openAIResult: &response.OpenAICompatibilityResult{StatusCode: http.StatusOK},
	}
}

func TestFetchNormalizesNineSourcesInRegistryOrder(t *testing.T) {
	priority := 7
	disabled := false
	note := "primary codex"
	fetcher := successfulProviderFetcher()
	// Codex 同时放入精确 auth-index 重复项和缺 auth-index 无效项。
	fetcher.codexResult.Payload = []providerconfig.ProviderKeyConfig{
		{APIKey: "codex-key", Prefix: "codex-prefix", Name: "Codex Team", BaseURL: "https://codex.example/v1", AuthIndex: "codex-auth", Priority: &priority, Disabled: &disabled, Note: &note},
		{APIKey: "codex-duplicate", Prefix: "duplicate-prefix", Name: "Duplicate", BaseURL: "https://duplicate.example/v1", AuthIndex: "codex-auth"},
		{APIKey: "codex-invalid", Name: "Invalid"},
	}
	fetcher.xaiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "xai-key", Prefix: "xai-prefix", BaseURL: "https://api.x.ai/v1", AuthIndex: "xai-auth"}}
	fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "gemini-key", Prefix: "gemini-prefix", BaseURL: "https://gemini.example/v1", AuthIndex: "gemini-auth"}}
	fetcher.interactionsResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "interactions-key", Prefix: "interactions-prefix", BaseURL: "https://interactions.example/v1", AuthIndex: "interactions-auth"}}
	fetcher.claudeResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "claude-key", Prefix: "claude-prefix", Name: "Claude Team", BaseURL: "https://claude.example/v1", AuthIndex: "claude-auth"}}
	fetcher.vertexResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "vertex-key", Prefix: "vertex-prefix", BaseURL: "https://vertex.example/v1", AuthIndex: "vertex-auth"}}
	// CommandCode 缺 name 时必须保持原默认名 commandcode。
	fetcher.commandcodeResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "commandcode-key", Prefix: "commandcode-prefix", BaseURL: "https://commandcode.example/v1", AuthIndex: "commandcode-auth"}}
	fetcher.metaResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "meta-key", Prefix: "meta-prefix", BaseURL: "https://meta.example/v1", AuthIndex: "meta-auth", Priority: &priority, Disabled: &disabled, Note: &note}}
	// OpenAI provider 层字段必须传播到有效 key entry，空 key entry 必须跳过。
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Name: "OpenRouter", Prefix: "openrouter", BaseURL: "https://openrouter.ai/api/v1", APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "openai-key", AuthIndex: "openai-auth"}, {AuthIndex: "openai-invalid"}}}}

	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	// wantTypes 锁定用户指定 registry 顺序。
	wantTypes := []string{"codex", "xai", "gemini", "gemini-interactions", "claude", "vertex", "commandcode", "meta", "openai"}
	// 实际 fetched types 必须与 registry 完全一致。
	if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
		t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
	}
	// wantCredentials 逐字段锁定九个有效 Credential 的来源和顺序。
	wantCredentials := []providermetadata.Credential{
		{LookupKey: "codex-key", Prefix: "codex-prefix", ProviderType: "codex", DisplayName: "Codex Team", AuthIndex: "codex-auth", BaseURL: "https://codex.example/v1", Priority: &priority, Disabled: &disabled, Note: &note},
		{LookupKey: "xai-key", Prefix: "xai-prefix", ProviderType: "xai", DisplayName: "xAI", AuthIndex: "xai-auth", BaseURL: "https://api.x.ai/v1"},
		{LookupKey: "gemini-key", Prefix: "gemini-prefix", ProviderType: "gemini", DisplayName: "gemini", AuthIndex: "gemini-auth", BaseURL: "https://gemini.example/v1"},
		{LookupKey: "interactions-key", Prefix: "interactions-prefix", ProviderType: "gemini-interactions", DisplayName: "Gemini Interactions", AuthIndex: "interactions-auth", BaseURL: "https://interactions.example/v1"},
		{LookupKey: "claude-key", Prefix: "claude-prefix", ProviderType: "claude", DisplayName: "Claude Team", AuthIndex: "claude-auth", BaseURL: "https://claude.example/v1"},
		{LookupKey: "vertex-key", Prefix: "vertex-prefix", ProviderType: "vertex", DisplayName: "vertex", AuthIndex: "vertex-auth", BaseURL: "https://vertex.example/v1"},
		// CommandCode 保持 commandcode 正确拼写。
		{LookupKey: "commandcode-key", Prefix: "commandcode-prefix", ProviderType: "commandcode", DisplayName: "commandcode", AuthIndex: "commandcode-auth", BaseURL: "https://commandcode.example/v1"},
		{LookupKey: "meta-key", Prefix: "meta-prefix", ProviderType: "meta", DisplayName: "Meta", AuthIndex: "meta-auth", BaseURL: "https://meta.example/v1", Priority: &priority, Disabled: &disabled, Note: &note},
		// OpenAI entry 接收 provider 层字段。
		{LookupKey: "openai-key", Prefix: "openrouter", ProviderType: "openai", DisplayName: "OpenRouter", AuthIndex: "openai-auth", BaseURL: "https://openrouter.ai/api/v1"},
	}
	if !reflect.DeepEqual(snapshot.Credentials, wantCredentials) {
		t.Fatalf("Credentials = %#v, want %#v", snapshot.Credentials, wantCredentials)
	}
}

func TestFetchPropagatesOpenAIProviderFieldsToEveryValidEntry(t *testing.T) {
	priority := 3
	disabled := true
	note := "shared provider"
	fetcher := successfulProviderFetcher()
	// OpenAI provider 故意缺 name，两个有效 entry 应共同使用默认名 openai。
	fetcher.openAIResult.Payload = []providerconfig.OpenAICompatibilityConfig{{Prefix: "shared-prefix", BaseURL: "https://shared.example/v1", Priority: &priority, Disabled: &disabled, Note: &note, APIKeyEntries: []providerconfig.OpenAIApiKeyEntry{{APIKey: "first-key", AuthIndex: "first-auth"}, {APIKey: "second-key", AuthIndex: "second-auth"}, {APIKey: "missing-auth"}}}}

	snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(snapshot.Credentials) != 2 {
		t.Fatalf("Credentials = %#v", snapshot.Credentials)
	}
	for index, want := range []struct {
		key       string
		authIndex string
	}{
		{key: "first-key", authIndex: "first-auth"},
		{key: "second-key", authIndex: "second-auth"},
	} {
		credential := snapshot.Credentials[index]
		if credential.LookupKey != want.key || credential.AuthIndex != want.authIndex || credential.ProviderType != "openai" || credential.DisplayName != "openai" || credential.Prefix != "shared-prefix" || credential.BaseURL != "https://shared.example/v1" || credential.Priority != &priority || credential.Disabled != &disabled || credential.Note != &note {
			t.Fatalf("credential[%d] = %#v", index, credential)
		}
	}
}

func TestFetchClassifiesOptionalFailuresAndSuccessfulEmptySources(t *testing.T) {
	t.Run("optional typed 404", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.xaiResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.xaiErr = errors.New("xai endpoint unavailable")
		fetcher.interactionsResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.interactionsErr = errors.New("interactions endpoint unavailable")

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}
		// optional 来源不进入 fetched types，其余成功来源仍按 registry 顺序保留。
		wantTypes := []string{"codex", "gemini", "claude", "vertex", "commandcode", "meta", "openai"}
		// fetched types 必须精确匹配剩余成功来源。
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
		}
	})

	// 只有 typed result 的 404 才能按 optional 处理，不能解析 error 文本。
	t.Run("nil result containing 404 text", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.xaiResult = nil
		fetcher.xaiErr = errors.New("request returned status 404")

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err == nil || err.Error() != "fetch xai api keys: request returned status 404" {
			t.Fatalf("error = %v", err)
		}
		// 失败 xAI 不进入 fetched types。
		if slices.Contains(snapshot.FetchedProviderTypes, "xai") {
			t.Fatalf("xai unexpectedly marked fetched: %#v", snapshot.FetchedProviderTypes)
		}
	})

	t.Run("stable warning order", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.codexResult = &response.ProviderKeyConfigResult{StatusCode: http.StatusNotFound}
		fetcher.codexErr = errors.New("codex missing")
		fetcher.geminiResult = nil
		fetcher.claudeErr = errors.New("claude decode failed")

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		wantError := "fetch codex api keys: codex missing; gemini api keys response is nil; fetch claude api keys: claude decode failed"
		if err == nil || err.Error() != wantError {
			t.Fatalf("error = %v, want %q", err, wantError)
		}
		// 失败来源不进入 fetched types，成功来源继续保留。
		wantTypes := []string{"xai", "gemini-interactions", "vertex", "commandcode", "meta", "openai"}
		// fetched types 必须精确匹配成功来源。
		if !reflect.DeepEqual(snapshot.FetchedProviderTypes, wantTypes) {
			t.Fatalf("FetchedProviderTypes = %#v, want %#v", snapshot.FetchedProviderTypes, wantTypes)
		}
	})

	t.Run("successful empty and invalid entries", func(t *testing.T) {
		fetcher := successfulProviderFetcher()
		fetcher.geminiResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "missing-auth"}, {AuthIndex: "missing-key"}}
		fetcher.metaResult.Payload = []providerconfig.ProviderKeyConfig{{APIKey: "meta-missing-auth"}, {AuthIndex: "meta-missing-key"}}

		snapshot, err := providermetadata.Fetch(context.Background(), fetcher)
		if err != nil {
			t.Fatalf("Fetch returned error: %v", err)
		}
		// 九个成功 endpoint 都必须进入 fetched types。
		if len(snapshot.FetchedProviderTypes) != 9 {
			t.Fatalf("FetchedProviderTypes = %#v", snapshot.FetchedProviderTypes)
		}
		if len(snapshot.Credentials) != 0 {
			t.Fatalf("Credentials = %#v", snapshot.Credentials)
		}
	})
}
