package inits

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Database() {
	newDb, err := gorm.Open(sqlite.Open("./database/database.sqlite"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Panicf("Failed to connect database: %s", err.Error())
	}

	DB = newDb
}
