package entities

import "time"

// CPAInstanceCredential stores only an irrecoverable ingest-token verifier.
type CPAInstanceCredential struct {
	ID         string     `gorm:"type:text;primaryKey"`
	InstanceID string     `gorm:"type:text;not null;index:idx_cpa_instance_credentials_instance_id"`
	Name       string     `gorm:"type:text;not null"`
	TokenHash  string     `gorm:"type:text;not null;uniqueIndex:uniq_cpa_instance_credentials_token_hash"`
	Scopes     string     `gorm:"type:text;not null"`
	ExpiresAt  *time.Time `gorm:"serializer:storageTime"`
	LastUsedAt *time.Time `gorm:"serializer:storageTime"`
	RevokedAt  *time.Time `gorm:"serializer:storageTime"`
	CreatedAt  time.Time  `gorm:"serializer:storageTime;not null"`
	UpdatedAt  time.Time  `gorm:"serializer:storageTime;not null"`
}

// CPAUsageDelivery is the immutable replay ledger for accepted push events.
// InboxID is intentionally not a foreign key: inbox rows are lifecycle data
// and may be deleted after processing, while replay history must remain intact.
type CPAUsageDelivery struct {
	InstanceID    string    `gorm:"type:text;primaryKey"`
	StreamID      string    `gorm:"type:text;primaryKey"`
	Sequence      uint64    `gorm:"primaryKey"`
	PayloadDigest []byte    `gorm:"type:blob;not null"`
	InboxID       int64     `gorm:"not null"`
	AcceptedAt    time.Time `gorm:"serializer:storageTime;not null"`
}

// CPAUsageStreamWatermark stores the highest contiguous accepted sequence.
type CPAUsageStreamWatermark struct {
	InstanceID          string    `gorm:"type:text;primaryKey"`
	StreamID            string    `gorm:"type:text;primaryKey"`
	AcknowledgedThrough uint64    `gorm:"not null;default:0"`
	CreatedAt           time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt           time.Time `gorm:"serializer:storageTime;not null"`
}
