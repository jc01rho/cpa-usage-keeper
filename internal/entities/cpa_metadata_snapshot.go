package entities

import "time"

// CPAMetadataSnapshot records the last committed complete snapshot for one
// credential-bound instance and fixed metadata category.
type CPAMetadataSnapshot struct {
	InstanceID  string    `gorm:"type:text;primaryKey"`
	Category    string    `gorm:"type:text;primaryKey"`
	Revision    int64     `gorm:"not null"`
	BodyDigest  []byte    `gorm:"type:blob;not null"`
	ItemCount   int64     `gorm:"not null"`
	GeneratedAt time.Time `gorm:"serializer:storageTime;not null"`
	AppliedAt   time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt   time.Time `gorm:"serializer:storageTime;not null"`
}
