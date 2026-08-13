package controllers

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"ch/kirari04/videocms/storage"

	"github.com/labstack/echo/v4"
)

type mediaTraffic struct {
	userID    uint
	fileID    uint
	qualityID uint
	audioID   uint
}

func (h *Handlers) readMediaObject(c echo.Context, storeID string, key storage.Key, maxBytes int64) ([]byte, error) {
	if h == nil || h.Deps == nil || h.Deps.Storage == nil {
		return nil, storage.ErrStoreNotConfigured
	}
	store, err := h.Deps.Storage.StoreOrDefault(storeID)
	if err != nil {
		return nil, err
	}
	object, err := store.Open(c.Request().Context(), key)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	if maxBytes > 0 && object.Info.Size > maxBytes {
		return nil, fmt.Errorf("media object %s exceeds read limit", key.String())
	}
	reader := io.Reader(object.Body)
	if maxBytes > 0 {
		reader = io.LimitReader(object.Body, maxBytes+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("media object %s exceeds read limit", key.String())
	}
	return data, nil
}

func (h *Handlers) serveMediaObject(c echo.Context, storeID string, key storage.Key, notFoundMessage string, traffic mediaTraffic) error {
	if h == nil || h.Deps == nil || h.Deps.Storage == nil {
		c.Logger().Error("media storage is not configured")
		return mediaStorageResponse(c, http.StatusInternalServerError, "")
	}
	store, err := h.Deps.Storage.StoreOrDefault(storeID)
	if err != nil {
		c.Logger().Error("failed to resolve media storage", err)
		if errors.Is(err, storage.ErrStoreNotConfigured) {
			return mediaStorageResponse(c, http.StatusServiceUnavailable, "Media storage is currently unavailable")
		}
		return mediaStorageResponse(c, http.StatusInternalServerError, "")
	}
	object, err := store.Open(c.Request().Context(), key)
	if err != nil {
		if !errors.Is(err, storage.ErrNotFound) {
			c.Logger().Errorf("failed to open media object %s: %v", key.String(), err)
			return mediaStorageResponse(c, http.StatusInternalServerError, "")
		}
		return mediaStorageResponse(c, http.StatusNotFound, notFoundMessage)
	}
	defer object.Body.Close()

	if object.Info.ContentType != "" {
		c.Response().Header().Set("Content-Type", object.Info.ContentType)
	}
	if object.Info.CacheControl != "" {
		c.Response().Header().Set("Cache-Control", object.Info.CacheControl)
	}

	counter := &countingResponseWriter{ResponseWriter: c.Response().Writer}
	http.ServeContent(counter, c.Request(), key.String(), object.Info.ModTime, object.Body)
	if counter.bytes > 0 && (counter.status == http.StatusOK || counter.status == http.StatusPartialContent) {
		h.Logic.TrackTraffic(
			traffic.userID,
			traffic.fileID,
			traffic.qualityID,
			traffic.audioID,
			counter.bytes,
		)
	}
	return nil
}

func mediaStorageResponse(c echo.Context, status int, message string) error {
	if message == "" {
		return c.NoContent(status)
	}
	return c.String(status, message)
}
