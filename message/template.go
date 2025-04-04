package message

import (
	"bytes"
	"html/template"
)

func safeHTML(s string) template.HTML {
	return template.HTML(s)
}

const defaultTemplate = `<!DOCTYPE html><html lang="en" class="antialiased"><head><meta charSet="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" /><link rel="preconnect" href="https://fonts.googleapis.com"><link rel="preconnect" href="https://fonts.gstatic.com" crossorigin><link href="https://fonts.googleapis.com/css2?family=Geist+Mono:wght@100..900&family=Geist:wght@100..900&display=swap" rel="stylesheet"><meta name="robots" content="noindex" /><title>{{.Title}}</title><meta name="description" content="{{.Description}}" /><link rel="icon" href="https://agentuity.com/favicon.ico" type="image/x-icon" sizes="48x48" /><link rel="icon" href="https://agentuity.com/icon.png" type="image/png" sizes="96x96" /><link rel="apple-touch-icon" href="https://agentuity.com/apple-icon.png" type="image/png" sizes="180x180" /><style type="text/css">body {font-family: 'Geist', sans-serif;font-size: 16px;line-height: 1.5;background-color: #09090B;-webkit-font-smoothing: antialiased;-moz-osx-font-smoothing: grayscale;text-rendering: optimizeLegibility;color: #A1A1AA;margin: 0;}main {display: flex;flex-direction: column;align-items: center;justify-content: center;height: 100vh;width: 100vw;gap: 2rem;}h1 {font-size: 1.5rem;line-height: 2rem;margin: 0;color: #FFF;}p {margin: 0;}svg {width: 3.5rem;height: 3.5rem;margin: 0 auto;}section {display: flex;flex-direction: column;align-items: center;justify-content: center;gap: 0.25rem;}details {display: flex;flex-direction: column;align-items: center;justify-content: center;gap: 0.25rem;}details summary {font-size: 0.75rem;line-height: 1rem;color: #A1A1AA;cursor: pointer;}details pre {padding: 1rem;background-color: #161617;border-radius: 0.5rem;font-size: 0.75rem;line-height: 1rem;color: #FFF;overflow: auto;width: 50vw;max-height: 24rem;font-family: 'Geist Mono', monospace;}</style></head><body><main><svg role="img" aria-label="Agentuity" fill="#0FF" width="24" height="22" viewBox="0 0 24 22" xmlns="http://www.w3.org/2000/svg"><title>Agentuity</title><path fill-rule="evenodd" clip-rule="evenodd" d="M24 21.3349H0L3.4284 15.3894H0L0.872727 13.8622H19.6909L24 21.3349ZM5.19141 15.3894L2.6437 19.8076H21.3563L18.8086 15.3894H5.19141Z"></path><path fill-rule="evenodd" clip-rule="evenodd" d="M12 0.498535L17.1762 9.49853H20.6182L21.4909 11.0258H5.94545L12 0.498535ZM8.58569 9.49853L12 3.56193L15.4143 9.49853H8.58569Z"></path></svg><section>
<h1>{{.HeaderTitle}}</h1>
<p>{{- if .RawHTML -}}{{.RawHTML | safeHTML}}{{- else -}}{{.Message}}{{- end -}}</p>
</section>{{if .ShowDetails}}<details><summary>See details</summary>
<pre>{{.ErrorDetails}}</pre>
</details>{{end}}</main></body></html>`

type TemplateData struct {
	Title        string // Page title in <title> tag
	Description  string // Meta description
	HeaderTitle  string // H1 title displayed on the page
	Message      string // Message text displayed below the title (escaped)
	RawHTML      string // Raw HTML message content (not escaped)
	ShowDetails  bool   // Whether to show the details section
	ErrorDetails string // Error details to display in the details section
}

var DefaultErrorTemplate = template.Must(template.New("error").Funcs(template.FuncMap{
	"safeHTML": safeHTML,
}).Parse(defaultTemplate))

func RenderHTML(data TemplateData) (string, error) {
	var buf bytes.Buffer
	err := DefaultErrorTemplate.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func RenderErrorPage(title, message, details string) (string, error) {
	data := TemplateData{
		Title:        title,
		Description:  "Deploy, run, and scale autonomous agents on infrastructure designed for the future, not the past.",
		HeaderTitle:  title,
		Message:      message,
		ShowDetails:  details != "",
		ErrorDetails: details,
	}
	return RenderHTML(data)
}

func RenderErrorPageWithHTML(title, htmlMessage, details string) (string, error) {
	data := TemplateData{
		Title:        title,
		Description:  "Deploy, run, and scale autonomous agents on infrastructure designed for the future, not the past.",
		HeaderTitle:  title,
		RawHTML:      htmlMessage,
		ShowDetails:  details != "",
		ErrorDetails: details,
	}
	return RenderHTML(data)
}

func WithCustomTemplate(tmplContent string) (*template.Template, error) {
	return template.New("custom").Funcs(template.FuncMap{
		"safeHTML": safeHTML,
	}).Parse(tmplContent)
}

func RenderHTMLWithTemplate(tmpl *template.Template, data TemplateData) (string, error) {
	var buf bytes.Buffer
	err := tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}
