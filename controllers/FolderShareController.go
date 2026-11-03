package controllers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ch/kirari04/videocms/auth"
	"ch/kirari04/videocms/helpers"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/sharing"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const folderShareContextKey = "folder_share"

type folderShareResponse struct {
	ID               uint
	Token            string
	FolderID         uint
	Name             string
	AllowRead        bool
	AllowUpload      bool
	MaxVideoSize     int64
	MaxFolderSize    int64
	ReturnUploadLink bool
	RequireCaptcha   bool
	HasPassword      bool
	ExpiresAt        *time.Time
	URL              string
}

func shareResponse(share models.FolderShare) folderShareResponse {
	return folderShareResponse{
		ID: share.ID, Token: share.Token, FolderID: share.FolderID, Name: share.Name,
		AllowRead: share.AllowRead, AllowUpload: share.AllowUpload, RequireCaptcha: share.RequireCaptcha,
		HasPassword: share.PasswordHash != "", ExpiresAt: share.ExpiresAt, URL: "/s/" + share.Token,
		MaxVideoSize: share.MaxVideoSize, MaxFolderSize: share.MaxFolderSize, ReturnUploadLink: share.ReturnsUploadLink(),
	}
}

func (h *Handlers) ListFolderShares(c echo.Context) error {
	folderID, err := strconv.ParseUint(c.QueryParam("FolderID"), 10, 64)
	if err != nil || folderID == 0 {
		return c.String(http.StatusBadRequest, "FolderID is required")
	}
	var shares []models.FolderShare
	if err := h.Deps.DB.Where("user_id = ? AND folder_id = ?", c.Get("UserID"), folderID).
		Order("id DESC").Find(&shares).Error; err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	items := make([]folderShareResponse, 0, len(shares))
	for _, share := range shares {
		items = append(items, shareResponse(share))
	}
	return c.JSON(http.StatusOK, items)
}

func (h *Handlers) CreateFolderShare(c echo.Context) error {
	var request models.FolderShareWrite
	if status, err := helpers.Validate(c, &request); err != nil {
		return c.String(status, err.Error())
	}
	share := models.FolderShare{UserID: c.Get("UserID").(uint)}
	if err := h.applyFolderShare(&share, request); err != nil {
		return err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	share.Token = base64.RawURLEncoding.EncodeToString(random[:])
	if err := h.Deps.DB.Create(&share).Error; err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.JSON(http.StatusCreated, shareResponse(share))
}

func (h *Handlers) UpdateFolderShare(c echo.Context) error {
	share, err := h.ownedFolderShare(c)
	if err != nil {
		return err
	}
	var request models.FolderShareWrite
	if status, err := helpers.Validate(c, &request); err != nil {
		return c.String(status, err.Error())
	}
	if request.FolderID != share.FolderID {
		return c.String(http.StatusBadRequest, "A share cannot be moved to another folder")
	}
	if err := h.applyFolderShare(share, request); err != nil {
		return err
	}
	// Use an owner-scoped update rather than Save, which could recreate a share
	// concurrently revoked by the owner.
	result := h.Deps.DB.Model(&models.FolderShare{}).Where("id = ? AND user_id = ?", share.ID, share.UserID).
		Updates(map[string]any{
			"name": share.Name, "allow_read": share.AllowRead, "allow_upload": share.AllowUpload,
			"require_captcha": share.RequireCaptcha, "password_hash": share.PasswordHash,
			"access_version": share.AccessVersion, "expires_at": share.ExpiresAt,
			"max_video_size": share.MaxVideoSize, "max_folder_size": share.MaxFolderSize, "return_upload_link": share.ReturnUploadLink,
		})
	if result.Error != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	if result.RowsAffected == 0 {
		return c.NoContent(http.StatusNotFound)
	}
	return c.JSON(http.StatusOK, shareResponse(*share))
}

func (h *Handlers) DeleteFolderShare(c echo.Context) error {
	share, err := h.ownedFolderShare(c)
	if err != nil {
		return err
	}
	if err := h.Deps.DB.Delete(share).Error; err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) ownedFolderShare(c echo.Context) (*models.FolderShare, error) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "Invalid share ID")
	}
	var share models.FolderShare
	if err := h.Deps.DB.Where("id = ? AND user_id = ?", id, c.Get("UserID")).First(&share).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, echo.NewHTTPError(http.StatusNotFound, "Share was not found")
		}
		return nil, echo.ErrInternalServerError
	}
	return &share, nil
}

func (h *Handlers) applyFolderShare(share *models.FolderShare, request models.FolderShareWrite) error {
	if !request.AllowRead && !request.AllowUpload {
		return echo.NewHTTPError(http.StatusBadRequest, "Enable read or upload access")
	}
	if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now()) {
		return echo.NewHTTPError(http.StatusBadRequest, "Expiration must be in the future")
	}
	var folder models.Folder
	if err := h.Deps.DB.Where("id = ? AND user_id = ?", request.FolderID, share.UserID).First(&folder).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return echo.ErrInternalServerError
		}
		return echo.NewHTTPError(http.StatusNotFound, "Folder was not found")
	}
	if request.RequireCaptcha && !h.shareCaptchaConfigured() {
		return echo.NewHTTPError(http.StatusBadRequest, "Configure and enable CAPTCHA in server settings first")
	}
	if request.Password != nil {
		if len(*request.Password) > 72 {
			return echo.NewHTTPError(http.StatusBadRequest, "Password must be at most 72 bytes")
		}
		share.PasswordHash = ""
		if *request.Password != "" {
			hash, err := bcrypt.GenerateFromPassword([]byte(*request.Password), bcrypt.DefaultCost)
			if err != nil {
				return echo.ErrInternalServerError
			}
			share.PasswordHash = string(hash)
		}
	}
	share.FolderID, share.Name = request.FolderID, strings.TrimSpace(request.Name)
	share.AllowRead, share.AllowUpload = request.AllowRead, request.AllowUpload
	if request.MaxVideoSize != nil {
		share.MaxVideoSize = *request.MaxVideoSize
	}
	if request.MaxFolderSize != nil {
		share.MaxFolderSize = *request.MaxFolderSize
	}
	if request.ReturnUploadLink != nil {
		share.ReturnUploadLink = request.ReturnUploadLink
	}
	share.RequireCaptcha, share.ExpiresAt = request.RequireCaptcha, request.ExpiresAt
	share.AccessVersion = uuid.NewString()
	return nil
}

func (h *Handlers) shareCaptchaConfigured() bool {
	cfg := h.Config()
	if cfg.CaptchaEnabled == nil || !*cfg.CaptchaEnabled {
		return false
	}
	switch cfg.CaptchaType {
	case "recaptcha":
		return cfg.Captcha_Recaptcha_PrivateKey != "" && cfg.Captcha_Recaptcha_PublicKey != ""
	case "hcaptcha":
		return cfg.Captcha_Hcaptcha_PrivateKey != "" && cfg.Captcha_Hcaptcha_PublicKey != ""
	case "turnstile":
		return cfg.Captcha_Turnstile_PrivateKey != "" && cfg.Captcha_Turnstile_PublicKey != ""
	}
	return false
}

func (h *Handlers) publicFolderShare(c echo.Context) (*models.FolderShare, error) {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	share, err := sharing.Resolve(h.Deps.DB, c.Param("token"))
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, sharing.ErrUnavailable) {
			return nil, echo.ErrInternalServerError
		}
		return nil, echo.NewHTTPError(http.StatusNotFound, "This share is unavailable or has expired")
	}
	return share, nil
}

func (h *Handlers) folderShareUnlocked(c echo.Context, share *models.FolderShare) bool {
	if share.PasswordHash == "" && !share.RequireCaptcha {
		return true
	}
	cookie, err := c.Cookie(auth.FolderShareCookieName(share.Token))
	return err == nil && h.Auth.VerifyFolderShareToken(cookie.Value, share)
}

type sharedFolderItem struct {
	ID   uint
	Name string
}
type sharedFileItem struct {
	Name        string
	Size        int64
	Processing  bool
	Available   bool
	PlayURL     string
	DownloadURL string
}
type sharedFolderResponse struct {
	Name                 string
	AllowRead            bool
	AllowUpload          bool
	MaxVideoSize         int64
	MaxFolderSize        int64
	UploadBytesRemaining *int64
	ReturnUploadLink     bool
	PasswordRequired     bool
	RequireCaptcha       bool
	Locked               bool
	FolderID             uint
	Folders              []sharedFolderItem
	Files                []sharedFileItem
}

func (h *Handlers) GetSharedFolder(c echo.Context) error {
	share, err := h.publicFolderShare(c)
	if err != nil {
		return err
	}
	response := sharedFolderResponse{
		AllowRead: share.AllowRead, AllowUpload: share.AllowUpload && h.Config().UploadEnabled != nil && *h.Config().UploadEnabled,
		PasswordRequired: share.PasswordHash != "", RequireCaptcha: share.RequireCaptcha,
		Locked: !h.folderShareUnlocked(c, share), Folders: []sharedFolderItem{}, Files: []sharedFileItem{},
	}
	if response.Locked {
		return c.JSON(http.StatusOK, response)
	}
	folderID := share.FolderID
	if raw := c.QueryParam("FolderID"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return c.String(http.StatusBadRequest, "Invalid folder ID")
		}
		folderID = uint(id)
	}
	// Upload-only shares expose only the destination name, never the subtree.
	if !share.AllowRead && folderID != share.FolderID {
		return c.String(http.StatusForbidden, "This link allows uploads only")
	}
	if err := sharing.ContainsFolder(h.Deps.DB, share, folderID); err != nil {
		return c.String(http.StatusForbidden, "Folder is outside this share")
	}
	var folder models.Folder
	if err := h.Deps.DB.First(&folder, folderID).Error; err != nil {
		return echo.ErrInternalServerError
	}
	response.Name, response.FolderID = folder.Name, folder.ID
	response.MaxVideoSize = h.Config().MaxUploadFilesize
	if share.MaxVideoSize > 0 && (response.MaxVideoSize <= 0 || share.MaxVideoSize < response.MaxVideoSize) {
		response.MaxVideoSize = share.MaxVideoSize
	}
	response.MaxFolderSize, response.ReturnUploadLink = share.MaxFolderSize, share.ReturnsUploadLink()
	if share.MaxFolderSize > 0 {
		capacity, err := sharing.UploadCapacity(h.Deps.DB, share, "")
		if err != nil {
			return echo.ErrInternalServerError
		}
		response.UploadBytesRemaining = &capacity.Remaining
	}
	if !share.AllowRead {
		return c.JSON(http.StatusOK, response)
	}
	var folders []models.Folder
	if err := h.Deps.DB.Where("parent_folder_id = ? AND user_id = ?", folderID, share.UserID).Order("name ASC").Find(&folders).Error; err != nil {
		return echo.ErrInternalServerError
	}
	for _, child := range folders {
		response.Folders = append(response.Folders, sharedFolderItem{ID: child.ID, Name: child.Name})
	}
	status, files, err := h.Logic.ListFiles(folderID, share.UserID)
	if err != nil {
		return c.String(status, err.Error())
	}
	for _, file := range *files {
		base := "/share-videos/" + share.Token + "/" + file.UUID
		item := sharedFileItem{Name: file.Name, Size: file.Size, Processing: file.Processing, Available: file.Available, PlayURL: base}
		if h.downloadsEnabled() {
			item.DownloadURL = base + "/download"
		}
		response.Files = append(response.Files, item)
	}
	return c.JSON(http.StatusOK, response)
}

func (h *Handlers) UnlockFolderShare(c echo.Context) error {
	share, err := h.publicFolderShare(c)
	if err != nil {
		return err
	}
	password := c.FormValue("password")
	if len(password) > 72 || (share.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(share.PasswordHash), []byte(password)) != nil) {
		return c.String(http.StatusUnauthorized, "Incorrect password")
	}
	if share.RequireCaptcha {
		if !h.shareCaptchaConfigured() {
			return c.String(http.StatusServiceUnavailable, "CAPTCHA is temporarily unavailable")
		}
		valid, err := h.Auth.CaptchaValid(c)
		if err != nil || !valid {
			return c.String(http.StatusBadRequest, "Complete the CAPTCHA challenge")
		}
	}
	token, expires, err := h.Auth.GenerateFolderShareToken(share)
	if err != nil {
		return echo.ErrInternalServerError
	}
	c.SetCookie(&http.Cookie{
		Name: auth.FolderShareCookieName(share.Token), Value: token, Path: "/",
		Expires: expires, MaxAge: int(time.Until(expires).Seconds()),
		HttpOnly: true, Secure: h.requestIsHTTPS(c), SameSite: http.SameSiteLaxMode,
	})
	return c.NoContent(http.StatusNoContent)
}

func (h *Handlers) FinalizeSharedUpload(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	if h.TUS == nil {
		return c.String(http.StatusServiceUnavailable, "Upload processing is unavailable")
	}
	if _, _, status, err := h.TUS.EnqueueSharedFinalize(c.Request(), c.Param("token"), c.Param("upload_id")); err != nil {
		return c.String(status, err.Error())
	}
	// The receipt exposes no owner job data. Public links are retrieved separately
	// after processing, only when the share owner permits them.
	return c.JSON(http.StatusAccepted, echo.Map{"Status": "received", "Message": "Upload received and queued for processing"})
}

func (h *Handlers) SharedUploadStatus(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	if h.TUS == nil {
		return c.String(http.StatusServiceUnavailable, "Upload processing is unavailable")
	}
	result, status, err := h.TUS.SharedUploadStatus(c.Request(), c.Param("token"), c.Param("upload_id"))
	if err != nil {
		return c.String(status, err.Error())
	}
	return c.JSON(http.StatusOK, result)
}

// SharedVideo verifies current permissions and ancestry before serving the
// existing player. The media token carries the share version for later checks.
func (h *Handlers) SharedVideo(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		share, err := h.publicFolderShare(c)
		if err != nil {
			return err
		}
		if !share.AllowRead {
			return c.String(http.StatusForbidden, "This link allows uploads only")
		}
		if !h.folderShareUnlocked(c, share) {
			return c.Redirect(http.StatusSeeOther, "/s/"+share.Token)
		}
		if err := sharing.ContainsLink(h.Deps.DB, share, c.Param("UUID")); err != nil {
			return c.String(http.StatusNotFound, "Video is outside this share")
		}
		c.Set(folderShareContextKey, share)
		return next(c)
	}
}

func requestFolderShare(c echo.Context) *models.FolderShare {
	share, _ := c.Get(folderShareContextKey).(*models.FolderShare)
	return share
}

func (h *Handlers) requestMediaClaims(c echo.Context, link *models.Link) auth.MediaClaims {
	claims := buildMediaClaims(link)
	if share := requestFolderShare(c); share != nil {
		claims.ShareToken, claims.ShareVersion = share.Token, share.AccessVersion
	}
	return claims
}

func playerBaseURL(c echo.Context, linkUUID string) string {
	if share := requestFolderShare(c); share != nil {
		return fmt.Sprintf("/share-videos/%s/%s", share.Token, linkUUID)
	}
	return "/v/" + linkUUID
}
