package controllers

import (
	"bytes"
	"ch/kirari04/videocms/middlewares"
	"ch/kirari04/videocms/models"
	"ch/kirari04/videocms/services"
	"context"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
)

func (h *Handlers) DispatchPlayerWebhook(c echo.Context) error {
	claims, ok := middlewares.MediaClaims(c)
	if !ok || claims.LinkUUID != c.Param("UUID") {
		return c.NoContent(http.StatusUnauthorized)
	}
	hookID, err := strconv.ParseUint(c.Param("HOOKID"), 10, 64)
	if err != nil || hookID == 0 {
		return c.NoContent(http.StatusNotFound)
	}
	var link models.Link
	if err := h.Deps.DB.Where("uuid = ? AND user_id = ?", claims.LinkUUID, claims.UserID).First(&link).Error; err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	var hook models.Webhook
	if err := h.Deps.DB.Where("user_id = ?", link.UserID).First(&hook, hookID).Error; err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	var payload struct {
		Position float64 `json:"position"`
		Value    string  `json:"value"`
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 4096)
	if err := c.Bind(&payload); err != nil || payload.Position < 0 || math.IsNaN(payload.Position) || math.IsInf(payload.Position, 0) || len(payload.Value) > 512 {
		return c.NoContent(http.StatusBadRequest)
	}
	target, err := url.Parse(hook.Url)
	if err != nil || target.Host == "" || target.Scheme != "http" && target.Scheme != "https" || len(hook.ResField) > 50 || hook.ResField == "" {
		return c.NoContent(http.StatusBadRequest)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range [][2]string{{"uuid", link.UUID}, {"position", strconv.FormatFloat(payload.Position, 'f', -1, 64)}, {hook.ResField, payload.Value}} {
		if err := form.WriteField(field[0], field[1]); err != nil {
			return c.NoContent(http.StatusInternalServerError)
		}
	}
	if err := form.Close(); err != nil {
		return c.NoContent(http.StatusInternalServerError)
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), &body)
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	req.Header.Set(echo.HeaderContentType, form.FormDataContentType())
	client := h.webhookHTTPClient
	if client == nil {
		client = services.NewPublicHTTPClient()
		defer client.CloseIdleConnections()
	}
	res, err := client.Do(req)
	if err != nil {
		return c.NoContent(http.StatusBadGateway)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64*1024))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return c.NoContent(http.StatusBadGateway)
	}
	return c.NoContent(http.StatusNoContent)
}
