package controllers

import (
	"bytes"
	"errors"
	"regexp"

	"ch/kirari04/videocms/models"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

var (
	webPageMarkdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	)
	webPageSanitizer = newWebPageSanitizer()
)

func RenderWebPageContent(format string, content string) (string, error) {
	switch format {
	case models.WebPageFormatMarkdown:
		var rendered bytes.Buffer
		if err := webPageMarkdown.Convert([]byte(content), &rendered); err != nil {
			return "", err
		}
		return string(webPageSanitizer.SanitizeBytes(rendered.Bytes())), nil
	case models.WebPageFormatHTML:
		return webPageSanitizer.Sanitize(content), nil
	default:
		return "", errors.New("unsupported webpage format")
	}
}

func newWebPageSanitizer() *bluemonday.Policy {
	policy := bluemonday.UGCPolicy()
	policy.AllowElements("details", "summary", "figure", "figcaption", "mark")
	policy.AllowAttrs("id").
		Matching(regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_:.-]*$`)).
		Globally()
	policy.AllowAttrs("class").
		Matching(regexp.MustCompile(`^vc-[a-z0-9-]+(?:\s+vc-[a-z0-9-]+)*$`)).
		Globally()
	policy.RequireNoReferrerOnLinks(true)
	return policy
}
