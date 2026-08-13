package models

type File struct {
	Model
	UUID         string
	Hash         string `gorm:"size:128;" json:"-"`
	Thumbnail    string
	Size         int64
	Duration     float64
	AvgFrameRate float64
	Height       int64
	Width        int64
	StorageID    string `gorm:"size:64;index" json:"-"`
	StorageState string `gorm:"size:16;index;default:available"`
	SourceKey    string `gorm:"size:1024" json:"-"`
	Path         string `gorm:"size:120;" json:"-"`
	Folder       string `gorm:"size:120;" json:"-"`
	User         User   `json:"-"`
	UserID       uint
	Qualitys     []Quality  `json:"-"`
	Subtitles    []Subtitle `json:"-"`
	Audios       []Audio    `json:"-"`
	Links        []Link     `json:"-"`
}

type FileCreateValidation struct {
	Name           string `validate:"required,min=1,max=120"`
	ParentFolderID uint   `validate:"number"`
}

type FileCloneValidation struct {
	Name           string `validate:"required,min=1,max=120"`
	Sha256         string `validate:"required,sha256"`
	ParentFolderID uint   `validate:"number"`
}
