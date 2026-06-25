package cmd

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/config"
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"log"
	"os"
	"time"

	"github.com/patrickmn/go-cache"
)

func InitRuntime() (*app.Deps, error) {
	base := config.LoadEnv()

	if err := inits.EnsureFolders(base); err != nil {
		return nil, err
	}

	if errors := helpers.ValidateStruct(base); len(errors) > 0 {
		for _, err := range errors {
			log.Printf("%v", err)
		}
		return nil, os.ErrInvalid
	}

	db, err := inits.OpenDatabase("./database/database.sqlite")
	if err != nil {
		return nil, err
	}
	if err := inits.MigrateModels(db); err != nil {
		return nil, err
	}
	snapshot, err := configdb.LoadSnapshot(db, base)
	if err != nil {
		return nil, err
	}
	if err := inits.EnsureFolders(snapshot.Config); err != nil {
		return nil, err
	}

	return &app.Deps{
		DB:          db,
		Snapshots:   app.NewSnapshotStore(snapshot),
		Cache:       cache.New(5*time.Minute, 10*time.Minute),
		RequestGate: app.NewRequestGate(),
	}, nil
}
