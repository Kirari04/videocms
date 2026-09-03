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

type storageMountTestRequest struct {
	MountID       uint             `json:"mount_id"`
	Provider      string           `json:"provider"`
	Configuration json.RawMessage  `json:"configuration" validate:"required"`
	Credentials   *json.RawMessage `json:"credentials"`
}

type sftpHostKeyRequest struct {
	Host string `json:"host" validate:"required"`
	Port int    `json:"port"`
}

type storagePoolRequest struct {
	Name            string                     `json:"name" validate:"required,min=1,max=120"`
	MountIDs        []uint                     `json:"mount_ids"`
	PrimaryMountIDs []uint                     `json:"primary_mount_ids"`
	CacheMounts     []storageCacheMountRequest `json:"cache_mounts"`
	IsDefault       bool                       `json:"is_default"`
}

type storageCacheMountRequest struct {
	MountID  uint  `json:"mount_id"`
	MaxBytes int64 `json:"max_bytes"`
}

type storageReconnectRequest struct {
	Apply bool `json:"apply"`
}

func (h *Handlers) GetStorageAdminOverview(c echo.Context) error {
	includeTraffic := true
	if raw := c.QueryParam("include_traffic"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return c.String(http.StatusBadRequest, "include_traffic must be true or false")
		}
		includeTraffic = parsed
	}
	overview, err := h.Logic.StorageAdminOverviewWithTraffic(includeTraffic)
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

func (h *Handlers) TestStorageMount(c echo.Context) error {
	request := new(storageMountTestRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	if err := h.Logic.TestStorageMount(c.Request().Context(), request.MountID, logic.StorageMountInput{
		Provider:      request.Provider,
		Configuration: request.Configuration,
		Credentials:   request.Credentials,
	}); err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func (h *Handlers) ScanSFTPHostKey(c echo.Context) error {
	request := new(sftpHostKeyRequest)
	if status, err := helpers.Validate(c, request); err != nil {
		return c.String(status, err.Error())
	}
	hostKey, err := storage.ScanSFTPHostKey(c.Request().Context(), request.Host, request.Port)
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, hostKey)
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
		Name: request.Name, MountIDs: request.MountIDs, PrimaryMountIDs: request.PrimaryMountIDs,
		CacheMounts: storageCacheInputs(request.CacheMounts), IsDefault: request.IsDefault,
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
		Name: request.Name, MountIDs: request.MountIDs, PrimaryMountIDs: request.PrimaryMountIDs,
		CacheMounts: storageCacheInputs(request.CacheMounts), IsDefault: request.IsDefault,
	})
	if err != nil {
		return storageAdminError(c, err)
	}
	return c.JSON(http.StatusOK, pool)
}

func storageCacheInputs(requests []storageCacheMountRequest) []logic.StorageCacheMountInput {
	result := make([]logic.StorageCacheMountInput, 0, len(requests))
	for _, request := range requests {
		result = append(result, logic.StorageCacheMountInput{MountID: request.MountID, MaxBytes: request.MaxBytes})
	}
	return result
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
