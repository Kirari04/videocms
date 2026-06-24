package inits

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenDatabase(path string) (*gorm.DB, error) {
	newDb, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
		IgnoreRelationshipsWhenMigrating:         true,
	})
	if err != nil {
		return nil, err
	}

	if sqlDB, err := newDb.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	// Performance optimizations
	// Enable Write-Ahead Logging (WAL) for concurrency
	if res := newDb.Exec("PRAGMA journal_mode=WAL;"); res.Error != nil {
		log.Printf("Failed to enable WAL mode: %v", res.Error)
	}
	// Synchronous NORMAL is faster and safe enough for WAL
	newDb.Exec("PRAGMA synchronous=NORMAL;")
	// Increase busy timeout to wait for locks instead of failing immediately
	newDb.Exec("PRAGMA busy_timeout=5000;")
	// Store temp tables in memory
	newDb.Exec("PRAGMA temp_store=MEMORY;")
	// Increase cache size (approx 20MB, negative value is in KB)
	newDb.Exec("PRAGMA cache_size=-20000;")

	return newDb, nil
}
