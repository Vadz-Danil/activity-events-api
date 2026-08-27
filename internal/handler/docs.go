package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Vadz-Danil/activity-events-api/api"
)

const (
	openAPIMediaType = "application/yaml"
	docsMediaType    = "text/html; charset=utf-8"
)

const docsPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>activity-events-api</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>body { margin: 0 } .topbar { display: none }</style>
</head>
<body>
  <div id="ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: 'openapi.yaml',
      dom_id: '#ui',
      docExpansion: 'list',
      defaultModelsExpandDepth: 0,
      persistAuthorization: true,
    })
  </script>
</body>
</html>`

type Docs struct{}

func NewDocs() *Docs { return &Docs{} }

func (h *Docs) Spec(c *gin.Context) {
	c.Data(http.StatusOK, openAPIMediaType, api.OpenAPI)
}

func (h *Docs) Page(c *gin.Context) {
	c.Data(http.StatusOK, docsMediaType, []byte(docsPage))
}
