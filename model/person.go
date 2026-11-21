package model

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

type Gender string

const (
	UnKnown Gender = "unknow"
	Male    Gender = "male"
	Female  Gender = "female"
	Futa    Gender = "futa"
)

type Person struct {
	ID        uint                      `gorm:"primaryKey" json:"id"`
	RelIDs    pq.StringArray            `gorm:"type:text[];index:person_rel_ids,type:gin" json:"rel_ids"`
	Name      string                    `gorm:"type:varchar(255);index:people_name_idx" json:"name"`
	Alias     pq.StringArray            `gorm:"type:text[]" json:"alias"`
	Cover     string                    `gorm:"type:varchar(512)" json:"cover"`
	Images    pq.StringArray            `gorm:"type:text[]" json:"images"`
	Summary   string                    `json:"summary"`
	Gender    Gender                    `gorm:"type:varchar(10)" json:"gender"`
	Links     datatypes.JSONSlice[Link] `gorm:"type:json" json:"links"`
	CreatedAt time.Time                 `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt time.Time                 `gorm:"autoUpdateTime:milli"`
}

func (Person) TableName() string {
	return "people"
}

type PersonTag struct {
	PersonID uint `gorm:"primaryKey"`
	TagID    uint `gorm:"primaryKey"`
}

func (PersonTag) TableName() string {
	return "person_tag"
}
