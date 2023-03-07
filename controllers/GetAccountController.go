package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetAccount(c *fiber.Ctx) error {
	userID := c.Locals("UserID").(uint)
	type Response struct {
		Username string
		Admin    bool
		Email    string
		Balance  float64
		Storage  int64
		Used     int64
		Files    int64
	}
	// cached response
	if data, found := inits.Cache.Get(fmt.Sprintf("account-%d", userID)); found {
		return c.JSON(data.(*Response))
	}

	var dbUser models.User
	if res := inits.DB.Find(&dbUser, userID); res.Error != nil {
		log.Printf("Failed to query user: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	type DBResponse struct {
		UploadedFiles int64
		StorageUsed   int64
	}
	var dbUsed DBResponse
	if res := inits.DB.Model(&models.Link{}).
		Joins("inner join files on files.id = links.file_id").
		Select("COUNT(links.id) AS uploaded_files", "SUM(files.size) AS storage_used").
		Where(&models.Link{
			UserID: userID,
		}).
		Group("links.user_id").
		First(&dbUsed); res.Error != nil {
		log.Printf("Failed to query UploadedFiles & StorageUsed: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}
	newResponse := Response{
		Username: dbUser.Username,
		Admin:    dbUser.Admin,
		Email:    dbUser.Email,
		Balance:  dbUser.Balance,
		Storage:  dbUser.Storage,
		Used:     dbUsed.StorageUsed,
		Files:    dbUsed.UploadedFiles,
	}
	// save in cache
	inits.Cache.Set(fmt.Sprintf("account-%d", userID), &newResponse, time.Minute)

	return c.JSON(newResponse)
}
