package controllers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/logic"
	"ch/kirari04/videocms/storage"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

type storageMountRequest struct {
	Name          string           `json:"name" validate:"required,min=1,max=120"`
	Provider      string           `json:"provider"`
	Configuration json.RawMessage  `json:"configuration" validate:"required"`
	Credentials   *json.RawMessage `json:"credentials"`
}

type storagePoolRequest struct {
	Name      string `json:"name" validate:"required,min=1,max=120"`
	MountIDs  []uint `json:"mount_ids" validate:"required,min=1"`
	IsDefault bool   `json:"is_default"`
}

type storageReconnectRequest struct {
	Apply bool `json:"apply"`
}

func (h *Handlers) GetStorageAdminOverview(c echo.Context) error {
	overview, err := h.Logic.StorageAdminOverview()
	if err != nil {
		return c.String(http.StatusInternalServerError, err.Error())
	}
	return c.JSON(http.StatusOK, overview)
}

func (h *Handlers) CreateStorageMount(c echo.Context) error {
	request := new(storageMountRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	mount, reconnect, err := h.Logic.CreateStorageMount(c.Request().Context(), logic.StorageMountInput{
		Name:          request.Name,
		Provider:      request.Provider,
		Configuration: request.Configuration,
		Credentials:   request.Credentials,
	})
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusCreated, map[string]any{"mount": mount, "reconnect": reconnect})
}

func (h *Handlers) UpdateStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	request := new(storageMountRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	mount, err := h.Logic.UpdateStorageMount(c.Request().Context(), mountID, logic.StorageMountInput{
		Name:          request.Name,
		Provider:      request.Provider,
		Configuration: request.Configuration,
		Credentials:   request.Credentials,
	})
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, mount)
}

func (h *Handlers) UnmountStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	unavailable, err := h.Logic.UnmountStorageMount(mountID)
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]int64{"unavailable_files": unavailable})
}

func (h *Handlers) DeleteStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	unavailable, err := h.Logic.DeleteStorageMount(mountID)
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]int64{"unavailable_files": unavailable})
}

func (h *Handlers) RemountStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	result, err := h.Logic.RemountStorageMount(c.Request().Context(), mountID)
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *Handlers) CheckStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if err := h.Logic.CheckStorageMount(c.Request().Context(), mountID); err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ReconnectStorageMount(c echo.Context) error {
	mountID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	request := new(storageReconnectRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	result, err := h.Logic.ReconnectStorageMount(c.Request().Context(), mountID, request.Apply)
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *Handlers) CreateStoragePool(c echo.Context) error {
	request := new(storagePoolRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	pool, err := h.Logic.CreateStoragePool(logic.StoragePoolInput{
		Name: request.Name, MountIDs: request.MountIDs, IsDefault: request.IsDefault,
	})
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusCreated, pool)
}

func (h *Handlers) UpdateStoragePool(c echo.Context) error {
	poolID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	request := new(storagePoolRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	pool, err := h.Logic.UpdateStoragePool(poolID, logic.StoragePoolInput{
		Name: request.Name, MountIDs: request.MountIDs, IsDefault: request.IsDefault,
	})
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, pool)
}

func (h *Handlers) SetDefaultStoragePool(c echo.Context) error {
	poolID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if err := h.Logic.SetDefaultStoragePool(poolID); err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) DeleteStoragePool(c echo.Context) error {
	poolID, err := storageResourceID(c)
	if err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	if err := h.Logic.DeleteStoragePool(poolID); err != nil {
		return storageAdminError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func storageResourceID(c echo.Context) (uint, error) {
	value, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || value == 0 {
		return 0, errors.New("invalid storage resource ID")
	}
	return uint(value), nil
}

func storageAdminError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, storage.ErrEncryptionKeyNotConfigured):
		return c.String(http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		return c.String(http.StatusNotFound, err.Error())
	default:
		return c.String(http.StatusBadRequest, err.Error())
	}
}
