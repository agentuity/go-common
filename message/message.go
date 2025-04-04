package message

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
)

func ErrorResponse(w http.ResponseWriter, title string, message string, details string, statusCode int) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)

	html, err := RenderErrorPage(title, message, details)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, html)
	return err
}

func NotFoundResponse(w http.ResponseWriter) error {
	return ErrorResponse(w, "Page not found", "The page you are looking for does not exist.", "", http.StatusNotFound)
}

func ServerErrorResponse(w http.ResponseWriter, err error) error {
	details := ""
	if err != nil {
		details = err.Error()
	}
	return ErrorResponse(w, "Server Error", "An unexpected error occurred.", details, http.StatusInternalServerError)
}

func CustomErrorResponse(w http.ResponseWriter, title string, message string, details string, statusCode int) error {
	return ErrorResponse(w, title, message, details, statusCode)
}

type TemplateWriter struct {
	Template *template.Template
}

func NewTemplateWriter() *TemplateWriter {
	return &TemplateWriter{
		Template: DefaultErrorTemplate,
	}
}

func (tw *TemplateWriter) WithTemplate(tmpl *template.Template) *TemplateWriter {
	tw.Template = tmpl
	return tw
}

func (tw *TemplateWriter) Write(w http.ResponseWriter, data TemplateData, statusCode int) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)

	html, err := tw.RenderToString(data)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, html)
	return err
}

func (tw *TemplateWriter) RenderToString(data TemplateData) (string, error) {
	var buf bytes.Buffer
	err := tw.Template.Execute(&buf, data)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
