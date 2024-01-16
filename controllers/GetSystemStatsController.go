package controllers

import (
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"fmt"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

type StatItem struct {
	CreatedAt time.Time
	Cpu       float64
	Mem       float64
	NetOut    float64
	NetIn     float64
	DiskW     float64
	DiskR     float64
}

func GetSystemStats(c *fiber.Ctx) error {
	var validatus models.SystemResourceGetValidation
	if err := c.QueryParser(&validatus); err != nil {
		return c.Status(fiber.StatusBadRequest).SendString("Invalid body request format")
	}
	if errors := helpers.ValidateStruct(validatus); len(errors) > 0 {
		return c.Status(fiber.StatusBadRequest).SendString(fmt.Sprintf("%s [%s] : %s", errors[0].FailedField, errors[0].Tag, errors[0].Value))
	}

	amount := 48
	duration := time.Minute * 5
	if validatus.Interval == "5min" {
		amount = 48
		duration = time.Minute * 5
	}
	if validatus.Interval == "1h" {
		amount = 24
		duration = time.Hour
	}
	if validatus.Interval == "7h" {
		amount = 24
		duration = time.Hour * 7
	}
	var response []StatItem
	for i := 0; i < amount; i++ {
		var resources StatItem
		addFrom := duration * time.Duration(amount-(i)) * -1
		from := time.Now().Add(addFrom)
		addUntil := duration * time.Duration(amount-(i+1)) * -1
		until := time.Now().Add(addUntil)
		if res := inits.DB.
			Model(&models.SystemResource{}).
			Select(
				"created_at as created_at",
				"AVG(cpu) as cpu",
				"AVG(mem) as mem",
				"AVG(net_out) as net_out",
				"AVG(net_in) as net_in",
				"AVG(disk_w) as disk_w",
				"AVG(disk_r) as disk_ru",
			).
			Where("created_at > ?", from).
			Where("created_at < ?", until).
			Where("server_id IS NULL").
			Find(&resources); res.Error != nil {
			log.Println("Failed to query stats", res.Error)
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		response = append(response, StatItem{
			CreatedAt: time.Now().Add(duration * time.Duration(amount-(i+1)) * -1),
			Cpu:       resources.Cpu,
			Mem:       resources.Mem,
			NetOut:    resources.NetOut,
			NetIn:     resources.NetIn,
			DiskW:     resources.DiskW,
			DiskR:     resources.DiskR,
		})
	}

	return c.JSON(&response)
}
