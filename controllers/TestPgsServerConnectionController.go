package controllers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ch/kirari04/videocms/helpers"

	"github.com/labstack/echo/v4"
)

const pgsServerConnectionTimeout = 5 * time.Second

type pgsServerConnectionRequest struct {
	URL string `json:"url" validate:"required,max=2048"`
}

type pgsServerConnectionResponse struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode,omitempty"`
	LatencyMS  int64  `json:"latencyMs,omitempty"`
}

func (h *Handlers) TestPgsServerConnection(c echo.Context) error {
	var validation pgsServerConnectionRequest
	if status, err := helpers.Validate(c, &validation); err != nil {
		return c.JSON(status, pgsServerConnectionResponse{
			OK:      false,
			Message: "Enter a valid server URL.",
		})
	}

	targetURL, err := validatePgsServerURL(validation.URL)
	if err != nil {
		return c.JSON(http.StatusBadRequest, pgsServerConnectionResponse{
			OK:      false,
			Message: err.Error(),
		})
	}

	requestContext, cancel := context.WithTimeout(c.Request().Context(), pgsServerConnectionTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodHead, targetURL, nil)
	if err != nil {
		return c.JSON(http.StatusBadRequest, pgsServerConnectionResponse{
			OK:      false,
			Message: "The server URL could not be tested.",
		})
	}

	startedAt := time.Now()
	response, err := pgsServerHTTPClient().Do(request)
	latency := time.Since(startedAt).Milliseconds()
	if err != nil {
		return c.JSON(http.StatusBadGateway, pgsServerConnectionResponse{
			OK:      false,
			Message: formatPgsServerConnectionError(err),
		})
	}
	defer response.Body.Close()

	return c.JSON(http.StatusOK, pgsServerConnectionResponse{
		OK:         true,
		Message:    fmt.Sprintf("Server reached (HTTP %d, %d ms).", response.StatusCode, latency),
		StatusCode: response.StatusCode,
		LatencyMS:  latency,
	})
}

func validatePgsServerURL(rawURL string) (string, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	parsedURL, err := url.ParseRequestURI(trimmedURL)
	if err != nil || parsedURL.Host == "" {
		return "", fmt.Errorf("Enter a valid server URL.")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("The server URL must use http or https.")
	}
	return parsedURL.String(), nil
}

func pgsServerHTTPClient() *http.Client {
	return &http.Client{
		Timeout: pgsServerConnectionTimeout,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func formatPgsServerConnectionError(err error) string {
	if err == nil {
		return "Could not connect to the server."
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return "Connection timed out after 5 seconds."
	}
	return "Could not connect to the server."
}
