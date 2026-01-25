package controllers

import (
	"ch/kirari04/videocms/logic"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func GetTopStorageStats(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	limit := 10
	results, err := logic.GetTopStorage(userID, limit, "files")
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}

func GetAdminTopStorageStats(c echo.Context) error {
	userIDQuery := c.QueryParam("user_id")
	var userID uint
	if userIDQuery != "" {
		if id, err := strconv.ParseUint(userIDQuery, 10, 32); err == nil {
			userID = uint(id)
		}
	}

	mode := c.QueryParam("mode")
	if mode == "" {
		mode = "users"
	}

	limit := 10
	results, err := logic.GetTopStorage(userID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}
