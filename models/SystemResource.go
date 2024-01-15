package models

type SystemResource struct {
	Model
	ServerID *uint `gorm:"index" json:"-"`
	Server   *Server
	Cpu      float64
	Mem      float64
	NetOut   uint64
	NetIn    uint64
	DiskW    uint64
	DiskR    uint64
}
