package model

import (
	"time"

	"github.com/lib/pq"
	"gorm.io/datatypes"
)

const REL_ID_CUSTOM = "custom"

type LinkType string

const (
	LinkTypeSource      LinkType = "source"       // 来源
	LinkTypeMv          LinkType = "mv"           // mv
	LinkTypeInfo        LinkType = "info"         // 介绍
	LinkTypeWorkThrough LinkType = "work_through" // 攻略
	LinkTypeOfficial    LinkType = "official"
	LinkTypeOther       LinkType = "other"
)

var LinkTypeMap = map[string]LinkType{
	"video": LinkTypeMv,
}

type Link struct {
	Name string   `json:"name"`
	Url  string   `json:"url"`
	Type LinkType `json:"type"`
}

type DownloadType string

const (
	DownloadMagnet = "magnet"
)

type DownloadInfo struct {
	Content string       `json:"content"`
	Type    DownloadType `json:"type"`
	Version string       `json:"version"`
}

type GameInstance struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	GameID    uint      `gorm:"uniqueIndex:uk_game_version" json:"game_id"`
	Version   string    `gorm:"uniqueIndex:uk_game_version;type:varchar(100)" json:"version"`
	Path      string    `gorm:"index:idx_game_path;type:varchar(255)" json:"path"`
	Size      int64     `json:"size"`
	Comment   string    `json:"comment"`
	CreatedAt time.Time `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime:milli"`
}

func (GameInstance) TableName() string {
	return "game_instances"
}

type Game struct {
	ID         uint                      `gorm:"primaryKey" json:"id"`
	RelIDs     pq.StringArray            `gorm:"type:text[];index:game_rel_ids,type:gin" json:"rel_ids"`
	Name       string                    `gorm:"type:varchar(255);index:game_name_idx" json:"name"`
	Alias      pq.StringArray            `gorm:"type:text[]" json:"alias"`
	Cover      string                    `gorm:"type:varchar(512)" json:"cover"`
	Images     pq.StringArray            `gorm:"type:text[]" json:"images"`
	CategoryID uint                      `gorm:"default:null" json:"-"`
	Price      string                    `gorm:"type:varchar(128)" json:"price"`
	IssueDate  time.Time                 `json:"issue_date"`
	Story      string                    `json:"story"`
	Links      datatypes.JSONSlice[Link] `gorm:"type:jsonb" json:"links"`
	OtherInfo  string                    `json:"other_info"`
	CreatedAt  time.Time                 `gorm:"autoCreateTime:milli" json:"created_at"`
	UpdatedAt  time.Time                 `gorm:"autoUpdateTime:milli"`

	Category *Category `gorm:"foreignKey:CategoryID" json:"category"`
	Series   []*Series `gorm:"many2many:game_series;" json:"series"`
	Brands   []*Brand  `gorm:"many2many:game_brands;" json:"brands"`
	Tags     []*Tag    `gorm:"many2many:game_tag;" json:"tags"`
}

func (Game) TableName() string {
	return "games"
}

type GameBrands struct {
	GameID  uint `gorm:"primaryKey"`
	BrandID uint `gorm:"primaryKey"`
}

func (GameBrands) TableName() string {
	return "game_brands"
}

type GameSeries struct {
	GameID   uint `gorm:"primaryKey"`
	SeriesID uint `gorm:"primaryKey"`
}

func (GameSeries) TableName() string {
	return "game_series"
}

type CharacterRelation string

const (
	CRelationMain  CharacterRelation = "main"
	CRelationMinor CharacterRelation = "minor"
	CRelationMob   CharacterRelation = "mob"
)

type GameCharacter struct {
	GameID      uint              `gorm:"primaryKey"`
	CharacterID uint              `gorm:"primaryKey"`
	Relation    CharacterRelation `gorm:"type:varchar(32)" json:"relation"`
	Character   *Character        `gorm:"-" json:"-"`
}

func (GameCharacter) TableName() string {
	return "game_character"
}

type GameTag struct {
	GameID uint `gorm:"primaryKey"`
	TagID  uint `gorm:"primaryKey"`
}

func (GameTag) TableName() string {
	return "game_tag"
}

type PersonRelation string

const (
	PRelationUnknown PersonRelation = "unknown"
	PRelationPainter PersonRelation = "painter"
	PRelationWriter  PersonRelation = "writer"
	PRelationCV      PersonRelation = "cv"
	PRelationMusic   PersonRelation = "music"
)

type GameStaff struct {
	GameID    uint                                `gorm:"primaryKey"`
	PersonID  uint                                `gorm:"primaryKey"`
	Relations datatypes.JSONSlice[PersonRelation] `gorm:"type:text[]"`
	Person    *Person                             `gorm:"-" json:"-"`
}

func (GameStaff) TableName() string {
	return "game_staff"
}
