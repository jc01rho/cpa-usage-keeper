package test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	keeperapp "cpa-usage-keeper/internal/app"
	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	repodto "cpa-usage-keeper/internal/repository/dto"
)

func TestPricingCatalogStartupFailsWhenPersistedSnapshotIsInvalid(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "invalid-pricing.db")
	seedDB, err := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	setting, err := repository.UpsertModelPriceSetting(seedDB, repodto.ModelPriceSettingInput{Model: "model-a", PromptPricePer1M: 1})
	if err != nil {
		t.Fatalf("seed model price: %v", err)
	}
	if err := seedDB.Create(&entities.ModelPriceRule{
		ModelPriceSettingID: setting.ID,
		Key:                 "provider",
		Value:               "openai",
		Multiplier:          2,
	}).Error; err != nil {
		t.Fatalf("seed invalid model price rule: %v", err)
	}
	seedSQL, err := seedDB.DB()
	if err != nil {
		t.Fatalf("load seed SQL DB: %v", err)
	}
	if err := seedSQL.Close(); err != nil {
		t.Fatalf("close seed database: %v", err)
	}

	application, err := keeperapp.NewWithConfig(pricingCatalogStartupConfig(databasePath))
	if application != nil {
		_ = application.Close()
		t.Fatal("expected invalid pricing snapshot to prevent App construction")
	}
	if err == nil || !strings.Contains(err.Error(), "pricing snapshot") {
		t.Fatalf("expected pricing snapshot startup error, got %v", err)
	}

	// 构造失败必须释放 reader/writer，随后应能立即重新打开同一个数据库。
	verificationDB, openErr := repository.OpenDatabase(config.Config{SQLitePath: databasePath})
	if openErr != nil {
		t.Fatalf("expected failed App construction to release database pools: %v", openErr)
	}
	verificationSQL, sqlErr := verificationDB.DB()
	if sqlErr == nil {
		_ = verificationSQL.Close()
	}
}

func pricingCatalogStartupConfig(databasePath string) config.Config {
	return config.Config{
		AppPort:                "invalid-port",
		CPABaseURL:             "https://cpa.example.com",
		CPAManagementKey:       "secret",
		RedisQueueIdleInterval: time.Second,
		MetadataSyncInterval:   30 * time.Second,
		SQLitePath:             databasePath,
		RequestTimeout:         5 * time.Second,
		LogLevel:               "info",
		LogFileEnabled:         false,
		LogRetentionDays:       7,
	}
}
