package controllers

import (
	"errors"
	"net/http"
	"strconv"

	"ch/kirari04/videocms/logic"

	"github.com/labstack/echo/v4"
	"gorm.io/gorm"
)

func (h *Handlers) PreviewStorageMigration(c echo.Context) error {
	request := new(logic.StorageMigrationInput)
	if err := c.Bind(request); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_request"})
	}
	preview, err := h.Logic.PreviewStorageMigration(c.Request().Context(), *request)
	if err != nil {
		return storageMigrationError(c, err)
	}
	return c.JSON(http.StatusOK, preview)
}

func (h *Handlers) CreateStorageMigration(c echo.Context) error {
	request := new(logic.StorageMigrationInput)
	if err := c.Bind(request); err != nil || request.SourcePoolID == 0 || request.DestinationPoolID == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid_request"})
	}
	actorID, actorName := backgroundActor(c)
	migration, job, err := h.Logic.StartStorageMigration(c.Request().Context(), *request, actorID, actorName)
	if err != nil {
		return storageMigrationError(c, err)
	}
	location := "/api/v2/admin/storage/migrations/" + migration.UUID
	c.Response().Header().Set("Location", location)
	c.Response().Header().Set("Retry-After", "2")
	return c.JSON(http.StatusAccepted, echo.Map{"migration": migration, "job": job, "retryAfterSeconds": 2})
}

func (h *Handlers) ListStorageMigrations(c echo.Context) error {
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	beforeID, _ := strconv.ParseUint(c.QueryParam("beforeId"), 10, 64)
	migrations, err := h.Logic.ListStorageMigrations(c.Request().Context(), limit, uint(beforeID))
	if err != nil {
		return storageMigrationError(c, err)
	}
	summary, err := h.Logic.GetStorageMigrationSummary(c.Request().Context())
	if err != nil {
		return storageMigrationError(c, err)
	}
	response := echo.Map{"migrations": migrations, "summary": summary}
	if len(migrations) == limit {
		response["nextBeforeId"] = migrations[len(migrations)-1].ID
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handlers) GetStorageMigration(c echo.Context) error {
	migration, err := h.Logic.GetStorageMigration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return storageMigrationError(c, err)
	}
	response := echo.Map{"migration": migration}
	if migration.BackgroundJobID != "" && h.Deps.Background != nil {
		if job, jobErr := h.Deps.Background.Job(c.Request().Context(), migration.BackgroundJobID, nil, true); jobErr == nil {
			response["job"] = job
		}
	}
	if migration.CleanupJobID != "" && h.Deps.Background != nil {
		if job, jobErr := h.Deps.Background.Job(c.Request().Context(), migration.CleanupJobID, nil, true); jobErr == nil {
			response["cleanupJob"] = job
		}
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handlers) ListStorageMigrationItems(c echo.Context) error {
	migration, err := h.Logic.GetStorageMigration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return storageMigrationError(c, err)
	}
	limit, _ := strconv.Atoi(c.QueryParam("limit"))
	if limit < 1 || limit > 500 {
		limit = 100
	}
	afterID, _ := strconv.ParseUint(c.QueryParam("afterId"), 10, 64)
	items, err := h.Logic.ListStorageMigrationItems(c.Request().Context(), migration.ID, c.QueryParam("status"), limit, uint(afterID))
	if err != nil {
		return storageMigrationError(c, err)
	}
	response := echo.Map{"items": items}
	if len(items) == limit {
		response["nextAfterId"] = items[len(items)-1].ID
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handlers) KeepStorageMigrationOriginals(c echo.Context) error {
	actorID, actorName := backgroundActor(c)
	migration, err := h.Logic.KeepStorageMigrationOriginals(c.Request().Context(), c.Param("id"), actorID, actorName)
	if err != nil {
		return storageMigrationError(c, err)
	}
	return c.JSON(http.StatusOK, migration)
}

func (h *Handlers) CancelFailedStorageMigration(c echo.Context) error {
	migration, err := h.Logic.CancelFailedStorageMigration(c.Request().Context(), c.Param("id"))
	if err != nil {
		return storageMigrationError(c, err)
	}
	return c.JSON(http.StatusAccepted, migration)
}

func storageMigrationError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return c.JSON(http.StatusNotFound, echo.Map{"error": "storage_migration_not_found"})
	case errors.Is(err, logic.ErrStorageMigrationConflict):
		return c.JSON(http.StatusConflict, echo.Map{"error": "storage_migration_conflict", "message": err.Error()})
	case errors.Is(err, logic.ErrStorageMigrationEmpty):
		return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": "storage_migration_empty", "message": err.Error()})
	case errors.Is(err, logic.ErrStorageMigrationUnavailable):
		return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": "storage_migration_unavailable", "message": err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "storage_migration_failed"})
	}
}
