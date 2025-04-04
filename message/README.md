# Message Package

The message package provides HTML templating functionality for rendering dynamic web pages.

## Usage

### Basic Error Page

```go
import (
    "net/http"
    "github.com/agentuity/go-common/message"
)

func handler(w http.ResponseWriter, r *http.Request) {
    message.NotFoundResponse(w)
}
```

### Custom Error Page

```go
import (
    "net/http"
    "github.com/agentuity/go-common/message"
)

func handler(w http.ResponseWriter, r *http.Request) {
    message.CustomErrorResponse(w, "Custom Error", "Something went wrong", "Error details here", http.StatusBadRequest)
}
```

### Using a Template Writer

```go
import (
    "net/http"
    "github.com/agentuity/go-common/message"
)

func handler(w http.ResponseWriter, r *http.Request) {
    tw := message.NewTemplateWriter()
    data := message.TemplateData{
        Title:        "Page Title",
        Description:  "Page description",
        HeaderTitle:  "Header Title",
        Message:      "Page message",
        ShowDetails:  true,
        ErrorDetails: "Error details",
    }
    tw.Write(w, data, http.StatusOK)
}
```

### Custom Template

```go
import (
    "net/http"
    "github.com/agentuity/go-common/message"
)

func handler(w http.ResponseWriter, r *http.Request) {
    customTemplate := "<!DOCTYPE html><html>...</html>"
    tmpl, err := message.WithCustomTemplate(customTemplate)
    if err != nil {
        // Handle error
    }
    
    tw := message.NewTemplateWriter().WithTemplate(tmpl)
    data := message.TemplateData{
        Title:        "Custom Page",
        HeaderTitle:  "Custom Header",
        Message:      "Custom message",
    }
    tw.Write(w, data, http.StatusOK)
}
```
