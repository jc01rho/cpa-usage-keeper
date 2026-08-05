package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"cpa-usage-keeper/internal/entities"
	"cpa-usage-keeper/internal/repository"
	"golang.org/x/crypto/argon2"
)

const (
	ScopeUsagePush    = "usage:push"
	ScopeMetadataPush = "metadata:push"
	ScopeIdentityTest = "identity:test"
	dummyTokenHash    = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$EkSi/pcQRqWwrfurmXqWC0EtGo/wWd2oBvQr6uNTyhk"
)

var (
	ErrInstanceNotFound   = errors.New("instance not found")
	ErrCredentialNotFound = errors.New("credential not found")
	ErrInvalidCredential  = errors.New("invalid credential")
	ErrInstanceDisabled   = errors.New("instance disabled")
	ErrLegacyInstance     = errors.New("legacy instance cannot be deleted")
	ErrActiveCredentials  = errors.New("revoke all credentials before deleting instance")
)

type InstanceStore interface {
	CreateWithCredential(context.Context, entities.CPAInstance, entities.CPAInstanceCredential) error
	List(context.Context) ([]entities.CPAInstance, error)
	Get(context.Context, string) (entities.CPAInstance, error)
	Update(context.Context, string, *string, *bool, time.Time) (entities.CPAInstance, error)
	CreateCredential(context.Context, string, entities.CPAInstanceCredential) error
	ListCredentials(context.Context, string) ([]entities.CPAInstanceCredential, error)
	CredentialByID(context.Context, string) (entities.CPAInstanceCredential, error)
	RotateCredential(context.Context, string, string, entities.CPAInstanceCredential, time.Time) error
	RevokeCredential(context.Context, string, string, time.Time) error
	Delete(context.Context, string) error
}

type CPAInstanceService struct {
	store InstanceStore
	now   func() time.Time
}

type CreateInstanceInput struct {
	DisplayName    string
	CredentialName string
	Scopes         []string
	ExpiresAt      *time.Time
}

type CredentialInput struct {
	Name      string
	Scopes    []string
	ExpiresAt *time.Time
}

type IssuedInstanceCredential struct {
	Instance   entities.CPAInstance
	Credential entities.CPAInstanceCredential
	Token      string
}

type AuthenticatedIngestCredential struct {
	Instance   entities.CPAInstance
	Credential entities.CPAInstanceCredential
	Scopes     []string
}

func NewCPAInstanceService(store InstanceStore) *CPAInstanceService {
	return &CPAInstanceService{store: store, now: time.Now}
}

func NewCPAInstanceServiceWithDB(dbStore *repository.CPAInstanceRepository) *CPAInstanceService {
	return NewCPAInstanceService(dbStore)
}

func (s *CPAInstanceService) Create(ctx context.Context, input CreateInstanceInput) (IssuedInstanceCredential, error) {
	now := s.now().UTC()
	instance := entities.CPAInstance{ID: newUUIDv7(now), DisplayName: input.DisplayName, Enabled: true, CreatedAt: now, UpdatedAt: now}
	credential, token, err := newCredential(instance.ID, input.CredentialName, input.Scopes, input.ExpiresAt, now)
	if err != nil {
		return IssuedInstanceCredential{}, err
	}
	if err := s.store.CreateWithCredential(ctx, instance, credential); err != nil {
		return IssuedInstanceCredential{}, err
	}
	return IssuedInstanceCredential{Instance: instance, Credential: credential, Token: token}, nil
}

func (s *CPAInstanceService) List(ctx context.Context) ([]entities.CPAInstance, error) {
	return s.store.List(ctx)
}

func (s *CPAInstanceService) Get(ctx context.Context, instanceID string) (entities.CPAInstance, error) {
	row, err := s.store.Get(ctx, instanceID)
	return row, mapStoreError(err)
}

func (s *CPAInstanceService) Update(ctx context.Context, instanceID string, displayName *string, enabled *bool) (entities.CPAInstance, error) {
	row, err := s.store.Update(ctx, instanceID, displayName, enabled, s.now().UTC())
	return row, mapStoreError(err)
}

func (s *CPAInstanceService) Issue(ctx context.Context, instanceID string, input CredentialInput) (IssuedInstanceCredential, error) {
	instance, err := s.Get(ctx, instanceID)
	if err != nil {
		return IssuedInstanceCredential{}, err
	}
	now := s.now().UTC()
	credential, token, err := newCredential(instanceID, input.Name, input.Scopes, input.ExpiresAt, now)
	if err != nil {
		return IssuedInstanceCredential{}, err
	}
	if err := s.store.CreateCredential(ctx, instanceID, credential); err != nil {
		return IssuedInstanceCredential{}, mapStoreError(err)
	}
	return IssuedInstanceCredential{Instance: instance, Credential: credential, Token: token}, nil
}

func (s *CPAInstanceService) ListCredentials(ctx context.Context, instanceID string) ([]entities.CPAInstanceCredential, error) {
	rows, err := s.store.ListCredentials(ctx, instanceID)
	return rows, mapStoreError(err)
}

func (s *CPAInstanceService) Rotate(ctx context.Context, instanceID, credentialID string, input CredentialInput) (IssuedInstanceCredential, error) {
	instance, err := s.Get(ctx, instanceID)
	if err != nil {
		return IssuedInstanceCredential{}, err
	}
	now := s.now().UTC()
	replacement, token, err := newCredential(instanceID, input.Name, input.Scopes, input.ExpiresAt, now)
	if err != nil {
		return IssuedInstanceCredential{}, err
	}
	if err := s.store.RotateCredential(ctx, instanceID, credentialID, replacement, now); err != nil {
		return IssuedInstanceCredential{}, mapStoreError(err)
	}
	return IssuedInstanceCredential{Instance: instance, Credential: replacement, Token: token}, nil
}

func (s *CPAInstanceService) Revoke(ctx context.Context, instanceID, credentialID string) error {
	return mapStoreError(s.store.RevokeCredential(ctx, instanceID, credentialID, s.now().UTC()))
}

func (s *CPAInstanceService) Delete(ctx context.Context, instanceID string) error {
	return mapStoreError(s.store.Delete(ctx, instanceID))
}

func (s *CPAInstanceService) Authenticate(ctx context.Context, token string) (AuthenticatedIngestCredential, error) {
	credentialID, secret, ok := parseToken(token)
	if !ok {
		return AuthenticatedIngestCredential{}, ErrInvalidCredential
	}
	matched, err := s.store.CredentialByID(ctx, credentialID)
	encodedHash := dummyTokenHash
	if err == nil {
		encodedHash = matched.TokenHash
	}
	verified := verifyToken(secret, encodedHash)
	if err != nil || !verified {
		return AuthenticatedIngestCredential{}, ErrInvalidCredential
	}
	now := s.now().UTC()
	if matched.RevokedAt != nil || (matched.ExpiresAt != nil && !matched.ExpiresAt.After(now)) {
		return AuthenticatedIngestCredential{}, ErrInvalidCredential
	}
	instance, err := s.Get(ctx, matched.InstanceID)
	if err != nil {
		return AuthenticatedIngestCredential{}, ErrInvalidCredential
	}
	if !instance.Enabled {
		return AuthenticatedIngestCredential{}, ErrInstanceDisabled
	}
	return AuthenticatedIngestCredential{Instance: instance, Credential: matched, Scopes: decodeScopes(matched.Scopes)}, nil
}

func CanonicalScopes(scopes []string) []string {
	order := map[string]int{ScopeUsagePush: 0, ScopeMetadataPush: 1, ScopeIdentityTest: 2}
	out := append([]string(nil), scopes...)
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}

func HasScope(scopes []string, required string) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

func newCredential(instanceID, name string, scopes []string, expiresAt *time.Time, now time.Time) (entities.CPAInstanceCredential, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return entities.CPAInstanceCredential{}, "", fmt.Errorf("generate ingest credential: %w", err)
	}
	credentialID := newUUIDv7(now)
	secret := base64.RawURLEncoding.EncodeToString(raw)
	token := credentialID + "." + secret
	hash, err := hashToken(secret)
	if err != nil {
		return entities.CPAInstanceCredential{}, "", err
	}
	canonical := CanonicalScopes(scopes)
	return entities.CPAInstanceCredential{ID: credentialID, InstanceID: instanceID, Name: name, TokenHash: hash, Scopes: strings.Join(canonical, " "), ExpiresAt: expiresAt, CreatedAt: now, UpdatedAt: now}, token, nil
}

func hashToken(token string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate credential salt: %w", err)
	}
	digest := argon2.IDKey([]byte(token), salt, 3, 64*1024, 2, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=2$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(digest)), nil
}

func verifyToken(token, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	expected, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(token), salt, 3, 64*1024, 2, 32)
	var diff byte
	for i := range actual {
		diff |= actual[i] ^ expected[i]
	}
	return diff == 0
}

func parseToken(token string) (string, string, bool) {
	credentialID, secret, ok := strings.Cut(token, ".")
	if !ok || len(credentialID) != 36 || len(secret) != 43 || strings.Contains(secret, ".") {
		return "", "", false
	}
	return credentialID, secret, true
}

func decodeScopes(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Fields(value)
}

func mapStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrCPAInstanceNotFound):
		return ErrInstanceNotFound
	case errors.Is(err, repository.ErrCPACredentialNotFound):
		return ErrCredentialNotFound
	case errors.Is(err, repository.ErrLegacyCPAInstance):
		return ErrLegacyInstance
	case errors.Is(err, repository.ErrActiveCPACredentials):
		return ErrActiveCredentials
	default:
		return err
	}
}

func newUUIDv7(now time.Time) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	ms := uint64(now.UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
