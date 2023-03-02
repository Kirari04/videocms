package encworker

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"os"
	"time"
)

func StartEncCleenup() {
	for {
		runEncCleenup()
		time.Sleep(time.Minute)
	}
}

/*
This function deletes the originally uploaded file after all qualitys and subtitles were encoded
*/
func runEncCleenup() {
	type PossibleDeleteTargets struct {
		ID                   int
		EncodedQualityCount  int
		EncodedSubtitleCount int
	}
	var dbFiles []PossibleDeleteTargets
	if res := inits.DB.
		Raw(`
		SELECT
			f.id,
			COUNT(q.id) as 'encoded_quality_count',
			COUNT(s.id) as 'encoded_subtitle_count' FROM files f
		INNER JOIN qualities q ON q.file_id = f.id
		INNER JOIN subtitles s ON s.file_id = f.id
		WHERE 	q.encoding = ? AND
				(q.ready = ? OR q.failed = ?) AND
				(s.ready = ? OR s.failed = ?) AND
				f.path != ""
		GROUP BY f.id`, 0, 1, 1).
		Scan(&dbFiles); res.Error != nil {
		log.Println(res.Error)
		return
	}

	for _, dbFile := range dbFiles {
		var realFile models.File
		if res := inits.DB.
			Preload("Qualitys").
			Preload("Subtitles").
			Find(&realFile, dbFile.ID); res.Error != nil {
			log.Printf("Couldn't find real file (delete candidate): Searcher ID %d inside database. Error: %v", dbFile.ID, res.Error)
			continue
		}
		// in case all qualitys are encoded or failed the original file can be deleted
		if len(realFile.Qualitys) == dbFile.EncodedQualityCount && len(realFile.Subtitles) == dbFile.EncodedSubtitleCount {
			if err := os.Remove(realFile.Path); err != nil {
				log.Printf("Failed to delete file from path (%v): %v", realFile.Path, err)
				continue
			}
			realFile.Path = ""
			inits.DB.Save(&realFile)
		}
	}
}
