package test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"cpa-usage-keeper/internal/config"
	"cpa-usage-keeper/internal/repository"
	"cpa-usage-keeper/internal/service"
)

func TestInstanceAuthenticateCachesVerificationButStillEnforcesRevocation(t *testing.T) {
	db, err := repository.OpenDatabase(config.Config{SQLitePath: filepath.Join(t.TempDir(), "keeper.db")})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	svc := service.NewCPAInstanceServiceWithDB(repository.NewCPAInstanceRepository(db))
	ctx := context.Background()
	issued, err := svc.Create(ctx, service.CreateInstanceInput{DisplayName: "cpa", CredentialName: "primary", Scopes: []string{service.ScopeUsagePush}})
	if err != nil {
		t.Fatal(err)
	}

	// A burst of concurrent pushes with one valid token must all authenticate.
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, authErr := svc.Authenticate(ctx, issued.Token)
			errs <- authErr
		}()
	}
	wg.Wait()
	close(errs)
	for authErr := range errs {
		if authErr != nil {
			t.Fatalf("concurrent authenticate: %v", authErr)
		}
	}

	// A wrong secret never hits the cache of the right one.
	credentialID, secret, _ := strings.Cut(issued.Token, ".")
	wrong := credentialID + "." + strings.Repeat("A", len(secret))
	if _, authErr := svc.Authenticate(ctx, wrong); !errors.Is(authErr, service.ErrInvalidCredential) {
		t.Fatalf("wrong secret: %v", authErr)
	}

	// Revocation is checked on every call even when the secret verification is cached.
	if err := svc.Revoke(ctx, issued.Instance.ID, issued.Credential.ID); err != nil {
		t.Fatal(err)
	}
	if _, authErr := svc.Authenticate(ctx, issued.Token); !errors.Is(authErr, service.ErrInvalidCredential) {
		t.Fatalf("revoked credential still authenticates: %v", authErr)
	}
}
