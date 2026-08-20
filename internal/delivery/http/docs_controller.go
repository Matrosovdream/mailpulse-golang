package http

import (
	"mailpulse/api"

	"github.com/gofiber/fiber/v2"
)

// DocsController serves the embedded OpenAPI description and a Swagger UI page
// that reads it.
//
// Both routes are public. The description names every endpoint, so on a
// deployment where that surface is not meant to be advertised, set
// WEB_DOCS_ENABLED=false and neither route is registered at all.
//
// The guard is in SetupDocsRoute rather than in these handlers, so a disabled
// instance never reaches this code: the request falls through to the /api group
// and gets the same 401 as any other unmatched path under it, which does not
// confirm that a docs route exists here at all.
type DocsController struct{}

func NewDocsController() *DocsController {
	return &DocsController{}
}

// Spec serves api/openapi.yaml verbatim. Point any OpenAPI tool at this URL.
func (c *DocsController) Spec(ctx *fiber.Ctx) error {
	ctx.Set(fiber.HeaderContentType, "application/yaml; charset=utf-8")
	return ctx.Send(api.OpenAPISpec)
}

// UI serves a Swagger UI page pointed at Spec.
//
// Swagger UI's assets come from a CDN rather than being vendored, so this page
// needs outbound network access from the browser, not from the server. The
// version is pinned to a major so a breaking release cannot change the page
// under us.
func (c *DocsController) UI(ctx *fiber.Ctx) error {
	ctx.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	return ctx.SendString(swaggerUIPage)
}

const swaggerUIPage = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex, nofollow">
  <title>MailPulse API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #fafafa; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js" crossorigin></script>
  <script>
    window.onload = function () {
      window.ui = SwaggerUIBundle({
        url: "/api/openapi.yaml",
        dom_id: "#swagger-ui",
        deepLinking: true,
        persistAuthorization: true,
        docExpansion: "none",
        defaultModelsExpandDepth: 0,
        tryItOutEnabled: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        layout: "BaseLayout"
      });
    };
  </script>
</body>
</html>
`
