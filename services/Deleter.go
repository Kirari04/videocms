package services

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"os"
	"time"
)

func Deleter() {
	for {
		runDeleter()
		time.Sleep(time.Second * 10)
	}
}

func runDeleter() {
	var todos []models.File

	if res := inits.DB.
		Model(&models.File{}).
		Preload("Qualitys").
		Preload("Subtitles").
		Preload("Audios").
		Unscoped().
		Where("deleted_at IS NOT NULL").
		Find(&todos, todos); res.Error != nil {
		log.Printf("Failed to query deleted files: %v", res.Error)
	}

	for _, todo := range todos {
		/**
		* check if all files qualities, subs & audios are not currently encoding because else there might be
		* parallel to the delete command an active ffmpeg conversion running
		 */
		encoding := false
		for _, quality := range todo.Qualitys {
			if quality.Encoding {
				encoding = true
			}
		}
		for _, audio := range todo.Audios {
			if audio.Encoding {
				encoding = true
			}
		}
		for _, sub := range todo.Subtitles {
			if sub.Encoding {
				encoding = true
			}
		}

		if encoding {
			// we will try again in the next loop (the encoding process may be finished until then)
			continue
		}

		// delete related stuff
		if res := inits.DB.
			Unscoped().
			Where(&models.Subtitle{
				FileID: todo.ID,
			}).
			Delete(&models.Subtitle{}); res.Error != nil {
			log.Printf("Failed to delete Subtitles from database: %v", res.Error)
			continue
		}

		if res := inits.DB.
			Unscoped().
			Where(&models.Audio{
				FileID: todo.ID,
			}).
			Delete(&models.Audio{}); res.Error != nil {
			log.Printf("Failed to delete Audios from database: %v", res.Error)
			continue
		}

		if res := inits.DB.
			Unscoped().
			Where(&models.Quality{
				FileID: todo.ID,
			}).
			Delete(&models.Quality{}); res.Error != nil {
			log.Printf("Failed to delete Qualities from database: %v", res.Error)
			continue
		}

		/**
		 * First delete the original file (it might still exists if some error happend or it didn't finished encoding yet)
		 * Then we can delete the folder too
		 */
		if todo.Path != "" {
			if stats, err := os.Stat(todo.Path); !os.IsNotExist(err) && !stats.IsDir() {
				if err := os.Remove(todo.Path); err != nil {
					log.Printf("Failed to delete original file: %v", err)
				}
			}
		}

		if stats, err := os.Stat(todo.Folder); !os.IsNotExist(err) && stats.IsDir() {
			if err := os.RemoveAll(todo.Folder); err != nil {
				log.Printf("Failed to delete folder of file: %v", err)
			}
		}

		// delete file from database
		if res := inits.DB.
			Unscoped().
			Delete(&todo); res.Error != nil {
			log.Printf("Failed to delete File from database: %v", res.Error)
			continue
		}
	}
}