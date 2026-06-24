package controllers

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetTopStorageStats(c echo.Context) error {
	userID := c.Get("UserID").(uint)
	limit := 10
	results, err := h.Logic.GetTopStorage(userID, limit, "files")
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}

func (h *Handlers) GetAdminTopStorageStats(c echo.Context) error {
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
	results, err := h.Logic.GetTopStorage(userID, limit, mode)
	if err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusOK, results)
}
