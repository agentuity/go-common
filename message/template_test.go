package message

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderErrorPage(t *testing.T) {
	html, err := RenderErrorPage("Test Error", "This is a test error message", "Error details here")

	assert.NoError(t, err)
	assert.Contains(t, html, "<title>Test Error</title>")
	assert.Contains(t, html, "<h1>Test Error</h1>")
	assert.Contains(t, html, "<p>This is a test error message</p>")
	assert.Contains(t, html, "Error details here")
}

func TestRenderErrorPageWithHTML(t *testing.T) {
	htmlContent := "<strong>Bold text</strong> and <em>italic text</em>"
	html, err := RenderErrorPageWithHTML("HTML Test", htmlContent, "Error details here")

	assert.NoError(t, err)
	assert.Contains(t, html, "<title>HTML Test</title>")
	assert.Contains(t, html, "<h1>HTML Test</h1>")
	assert.Contains(t, html, "<p><strong>Bold text</strong> and <em>italic text</em></p>")
	assert.Contains(t, html, "Error details here")

	assert.NotContains(t, html, "&lt;strong&gt;")
	assert.NotContains(t, html, "&lt;em&gt;")
}

func TestRenderErrorPageWithoutDetails(t *testing.T) {
	html, err := RenderErrorPage("Test Error", "This is a test error message", "")

	assert.NoError(t, err)
	assert.Contains(t, html, "<title>Test Error</title>")
	assert.Contains(t, html, "<h1>Test Error</h1>")
	assert.Contains(t, html, "<p>This is a test error message</p>")
	assert.NotContains(t, html, "<details>")
}

func TestWithCustomTemplate(t *testing.T) {
	customTmpl := `<!DOCTYPE html><html><head><title>{{.Title}}</title></head><body><h1>{{.HeaderTitle}}</h1><p>{{.Message}}</p>{{if .ShowDetails}}<div>{{.ErrorDetails}}</div>{{end}}</body></html>`

	tmpl, err := WithCustomTemplate(customTmpl)
	assert.NoError(t, err)

	data := TemplateData{
		Title:        "Custom Title",
		HeaderTitle:  "Custom Header",
		Message:      "Custom message",
		ShowDetails:  true,
		ErrorDetails: "Custom details",
	}

	html, err := RenderHTMLWithTemplate(tmpl, data)
	assert.NoError(t, err)
	assert.Contains(t, html, "<title>Custom Title</title>")
	assert.Contains(t, html, "<h1>Custom Header</h1>")
	assert.Contains(t, html, "<p>Custom message</p>")
	assert.Contains(t, html, "<div>Custom details</div>")
}

func TestErrorResponse(t *testing.T) {
	w := httptest.NewRecorder()
	err := ErrorResponse(w, "Test Error", "Test message", "Test details", 404)

	assert.NoError(t, err)
	assert.Equal(t, 404, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "<title>Test Error</title>")
	assert.Contains(t, w.Body.String(), "<h1>Test Error</h1>")
	assert.Contains(t, w.Body.String(), "<p>Test message</p>")
	assert.Contains(t, w.Body.String(), "Test details")
}

func TestTemplateWriter(t *testing.T) {
	tw := NewTemplateWriter()
	w := httptest.NewRecorder()

	data := TemplateData{
		Title:        "Writer Test",
		HeaderTitle:  "Writer Header",
		Message:      "Writer message",
		ShowDetails:  true,
		ErrorDetails: "Writer details",
	}

	err := tw.Write(w, data, 200)

	assert.NoError(t, err)
	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "<title>Writer Test</title>")
	assert.Contains(t, w.Body.String(), "<h1>Writer Header</h1>")
	assert.Contains(t, w.Body.String(), "<p>Writer message</p>")
	assert.Contains(t, w.Body.String(), "Writer details")
}

func TestTemplateWriterRenderToString(t *testing.T) {
	tw := NewTemplateWriter()

	data := TemplateData{
		Title:        "String Test",
		HeaderTitle:  "String Header",
		Message:      "String message",
		ShowDetails:  true,
		ErrorDetails: "String details",
	}

	html, err := tw.RenderToString(data)

	assert.NoError(t, err)
	assert.Contains(t, html, "<title>String Test</title>")
	assert.Contains(t, html, "<h1>String Header</h1>")
	assert.Contains(t, html, "<p>String message</p>")
	assert.Contains(t, html, "String details")
}
