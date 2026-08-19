package controllers

import (
	"ch/kirari04/videocms/helpers"
	"net/http"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) GetThumbnailData(c echo.Context) error {
	type Request struct {
		UUID string `validate:"required,uuid_rfc4122" param:"UUID"`
		FILE string `validate:"required" param:"FILE"`
	}
	var requestValidation Request
	if status, err := helpers.Validate(c, &requestValidation); err != nil {
		return c.String(status, err.Error())
	}

	_, object, err := h.Logic.ResolveThumbnailObject(requestValidation.FILE, requestValidation.UUID)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	if h.Deps.Storage == nil || h.Deps.Storage.Layout() == nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	key, err := h.Deps.Storage.Layout().Thumbnail(object.FileUUID, requestValidation.FILE)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	return h.serveMediaObject(c, object.StoreID, key, "", mediaTraffic{
		userID: object.UserID, fileID: object.FileID, poolID: object.PoolID,
	})
}
