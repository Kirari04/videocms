package controllers

import (
	"ch/kirari04/videocms/app"
	"ch/kirari04/videocms/configdb"
	"ch/kirari04/videocms/models"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) RotateSigningKeys(c echo.Context) error {
	var request struct {
		Confirm bool `json:"confirm"`
	}
	if err := c.Bind(&request); err != nil || !request.Confirm {
		return c.String(http.StatusBadRequest, "Confirm signing-key rotation")
	}
	h.settingsMu.Lock()
	defer h.settingsMu.Unlock()
	var snapshot app.Snapshot
	err := h.Deps.DB.Transaction(func(tx *gorm.DB) error {
		var setting models.Setting
		if err := tx.First(&setting).Error; err != nil {
			return err
		}
		var err error
		setting.JwtSecretKey, err = configdb.GenerateSigningKey()
		if err != nil {
			return err
		}
		setting.JwtMediaSecretKey, err = configdb.GenerateSigningKey()
		if err != nil {
			return err
		}
		if err := tx.Save(&setting).Error; err != nil {
			return err
		}
		snapshot, err = configdb.LoadSnapshot(tx, h.Config())
		return err
	})
	if err != nil {
		log.Println("Failed to rotate signing keys", err)
		return c.NoContent(http.StatusInternalServerError)
	}
	h.Deps.Snapshots.Replace(snapshot)
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.NoContent(http.StatusNoContent)
}
