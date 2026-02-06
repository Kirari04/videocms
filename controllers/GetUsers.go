package controllers

import (
	"ch/kirari04/videocms/logic"
	"github.com/labstack/echo/v4"
	"strconv"
)

func GetUsers(c echo.Context) error {
	page, _ := strconv.Atoi(c.QueryParam("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 {
		limit = 10
	}

	search := c.QueryParam("search")

	status, users, err := logic.GetUsers(page, limit, search)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(status, users)
}
