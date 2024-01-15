package controllers

import (
	"ch/kirari04/videocms/inits"
	"ch/kirari04/videocms/models"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
)

func GetSystemStats(c *fiber.Ctx) error {
	type StatItem struct {
		CreatedAt time.Time
		Cpu       float64
		Mem       float64
		NetOut    float64
		NetIn     float64
		DiskW     float64
		DiskR     float64
	}
	var response []StatItem
	for i := 0; i < 24; i++ {
		var resources StatItem
		addFromHours := time.Hour * time.Duration(24-(i)) * -1
		from := time.Now().Add(addFromHours)
		addUntilHours := time.Hour * time.Duration(24-(i+1)) * -1
		until := time.Now().Add(addUntilHours)
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
			CreatedAt: time.Now().Add(time.Hour * time.Duration(24-(i+1)) * -1),
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
