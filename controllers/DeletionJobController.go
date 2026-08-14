package controllers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"ch/kirari04/videocms/background"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

func (h *Handlers) enqueueDeletion(c echo.Context, linkIDs, folderIDs []uint) error {
	linkIDs = append([]uint(nil), linkIDs...)
	folderIDs = append([]uint(nil), folderIDs...)
	sort.Slice(linkIDs, func(i, j int) bool { return linkIDs[i] < linkIDs[j] })
	sort.Slice(folderIDs, func(i, j int) bool { return folderIDs[i] < folderIDs[j] })
	userID := c.Get("UserID").(uint)
	isAdmin, _ := c.Get("Admin").(bool)
	if len(linkIDs)+len(folderIDs) == 0 {
		return c.String(http.StatusBadRequest, "No content selected")
	}
	if int64(len(linkIDs)+len(folderIDs)) > h.Config().MaxItemsMultiDelete {
		return c.String(http.StatusBadRequest, "Max requested items exceeded")
	}
	if err := h.validateDeletionSelection(userID, isAdmin, linkIDs, folderIDs); err != nil {
		return c.String(http.StatusBadRequest, err.Error())
	}
	requestKey := strings.TrimSpace(c.Request().Header.Get("Idempotency-Key"))
	if requestKey == "" {
		requestKey = uuid.NewString()
	}
	ownerID := userID
	job, _, err := h.background().Enqueue(c.Request().Context(), background.JobSpec{
		Kind: "content.delete", Visibility: background.VisibilityUser, OwnerID: &ownerID,
		SubjectType: "selection", SubjectID: requestKey, IdempotencyKey: fmt.Sprintf("delete:%d:%s", userID, requestKey),
		Label: deletionLabel(len(linkIDs), len(folderIDs)),
		Tasks: []background.TaskSpec{{
			Kind: "content.delete", Queue: background.QueueStorage, Phase: "validating",
			Payload:   map[string]any{"linkIds": linkIDs, "folderIds": folderIDs, "userId": userID, "admin": isAdmin},
			DedupeKey: "delete", Priority: 40, Required: true, Weight: 100,
		}},
	})
	if err != nil {
		return backgroundAPIError(c, err)
	}
	return acceptedBackgroundJob(c, job)
}

// Legacy deletion handlers preserve the synchronous v1 contract while new
// clients use the durable /v2 endpoints.
func (h *Handlers) DeleteFileLegacy(c echo.Context) error {
	var validation models.LinkDeleteValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}
	admin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.DeleteFiles(&models.LinksDeleteValidation{LinkIDs: []models.LinkDeleteValidation{validation}}, c.Get("UserID").(uint), admin)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}

func (h *Handlers) DeleteFilesLegacy(c echo.Context) error {
	var validation models.LinksDeleteValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}
	admin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.DeleteFiles(&validation, c.Get("UserID").(uint), admin)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}

func (h *Handlers) DeleteFolderLegacy(c echo.Context) error {
	var validation models.FolderDeleteValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}
	admin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.DeleteFolders(&models.FoldersDeleteValidation{FolderIDs: []models.FolderDeleteValidation{validation}}, c.Get("UserID").(uint), admin)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}

func (h *Handlers) DeleteFoldersLegacy(c echo.Context) error {
	var validation models.FoldersDeleteValidation
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.String(status, err.Error())
	}
	admin, _ := c.Get("Admin").(bool)
	status, err := h.Logic.DeleteFolders(&validation, c.Get("UserID").(uint), admin)
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.NoContent(status)
}

func (h *Handlers) validateDeletionSelection(userID uint, isAdmin bool, linkIDs, folderIDs []uint) error {
	seen := map[string]struct{}{}
	for _, id := range linkIDs {
		key := fmt.Sprintf("link:%d", id)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("files must be distinct")
		}
		seen[key] = struct{}{}
		query := h.Deps.DB.Model(&models.Link{}).Where("id = ?", id)
		if !isAdmin {
			query = query.Where("user_id = ?", userID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 1 {
			return fmt.Errorf("link %d does not exist", id)
		}
	}
	for _, id := range folderIDs {
		key := fmt.Sprintf("folder:%d", id)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("folders must be distinct")
		}
		seen[key] = struct{}{}
		query := h.Deps.DB.Model(&models.Folder{}).Where("id = ?", id)
		if !isAdmin {
			query = query.Where("user_id = ?", userID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil || count != 1 {
			return fmt.Errorf("folder %d does not exist", id)
		}
	}
	return nil
}

func deletionLabel(files, folders int) string {
	switch {
	case folders == 0 && files == 1:
		return "Delete video"
	case folders == 0:
		return fmt.Sprintf("Delete %d videos", files)
	case files == 0 && folders == 1:
		return "Delete folder"
	case files == 0:
		return fmt.Sprintf("Delete %d folders", folders)
	default:
		return fmt.Sprintf("Delete %d videos and %d folders", files, folders)
	}
}
