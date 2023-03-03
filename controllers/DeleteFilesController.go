package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"

	"github.com/gofiber/fiber/v2"
)

func DeleteFilesController(c *fiber.Ctx) error {
	userID := c.Locals("UserID").(uint)
	if helpers.UserRequestAsyncObj.Blocked(userID) {
		return c.Status(fiber.StatusTooManyRequests).SendString("Wait until the previous delete request finished")
	}
	helpers.UserRequestAsyncObj.Start(userID)
	defer helpers.UserRequestAsyncObj.End(userID)

	// parse & validate request
	var fileValidation models.LinksDeleteValidation
	if err := c.BodyParser(&fileValidation); err != nil {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "none",
				Tag:         "none",
				Value:       "Invalid body request format",
			},
		})
	}
	if errors := helpers.ValidateStruct(fileValidation); len(errors) > 0 {
		return c.Status(400).JSON(errors)
	}

	if len(fileValidation.LinkIDs) == 0 {
		return c.Status(400).JSON([]helpers.ValidationError{
			{
				FailedField: "FileIDs",
				Tag:         "none",
				Value:       "Array is empty",
			},
		})
	}

	//check if requested files exists
	linkIdDeleteList := []uint{}
	for _, LinkValidation := range fileValidation.LinkIDs {
		if res := inits.DB.First(&models.Link{
			UserID: userID,
		}, LinkValidation.LinkID); res.Error != nil {
			return c.Status(400).JSON([]helpers.ValidationError{
				{
					FailedField: "FileID",
					Tag:         "exists",
					Value:       fmt.Sprintf("FileID (%d) doesn't exist", LinkValidation.LinkID),
				},
			})
		}
		linkIdDeleteList = append(linkIdDeleteList, LinkValidation.LinkID)
	}

	// delete files
	if res := inits.DB.Delete(&models.Link{}, linkIdDeleteList); res.Error != nil {
		log.Printf("Failed to delete links: %v", res.Error)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusOK)
}
