package entities

import "time"

const (
	// LegacyCPAInstanceID is the deterministic immutable namespace for all data
	// created before multi-instance Keeper support and for the existing pull path.
	LegacyCPAInstanceID   = "00000000-0000-7000-8000-000000000000"
	LegacyCPAInstanceName = "Legacy"
)

// CPAInstance is an immutable CPA source namespace managed by Keeper.
type CPAInstance struct {
	ID          string    `gorm:"type:text;primaryKey"`
	DisplayName string    `gorm:"type:text;not null"`
	Enabled     bool      `gorm:"not null;default:true"`
	CreatedAt   time.Time `gorm:"serializer:storageTime;not null"`
	UpdatedAt   time.Time `gorm:"serializer:storageTime;not null"`
}
