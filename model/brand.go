package model

import (
	"time"

	"gorm.io/datatypes"
)

type Brand struct {
	ID        uint                      `gorm:"primaryKey" json:"id"`
	Name      string                    `gorm:"type:varchar(255);unique" json:"name"`
	Links     datatypes.JSONSlice[Link] `gorm:"type:jsonb" json:"link"`
	CreatedAt time.Time                 `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt time.Time                 `gorm:"autoUpdateTime:milli"`
}

func (Brand) TableName() string {
	return "brands"
}
