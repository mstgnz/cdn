// Package router builds the service's HTTP surface on chi, in the order and
// with the semantics the fiber app had. scripts/e2e-golden is the contract.
package router

import (
	"html"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/mstgnz/cdn/handler"
	"github.com/mstgnz/cdn/pkg/audit"
	"github.com/mstgnz/cdn/pkg/config"
	"github.com/mstgnz/cdn/pkg/httpx"
	"github.com/mstgnz/cdn/pkg/middleware"
	"github.com/mstgnz/cdn/pkg/observability"
	"github.com/mstgnz/cdn/service"
)

// Deps is what New wires together.
type Deps struct {
	Image   handler.Image
	AWS     handler.AwsHandler
	Minio   handler.MinioHandler
	WS      handler.WebSocketHandler
	Archive handler.ArchiveHandler
	Health  *handler.HealthChecker

	GlobalLimiter func(http.Handler) http.Handler
	UploadLimiter func(http.Handler) http.Handler // unused when DisableUpload

	DisableGet, DisableUpload, DisableDelete bool
	FaviconFile                              string
}

// fiberMethods is fiber's method order, which is the order of an Allow header.
var fiberMethods = []string{
	http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete,
	http.MethodConnect, http.MethodOptions, http.MethodTrace, http.MethodPatch,
}

// New builds the router. The middleware order below is the order the fiber app
// registered things in, and it is behaviour, not style: which requests the rate
// limiters count, which get CORS headers, which are metered and which need a
// token all follow from it.
func New(d Deps) http.Handler {
	r := chi.NewRouter()

	// Metrics labels keep fiber's route strings, so dashboards keep working.
	labels := map[string]string{}
	add := func(method, pattern, label string, h http.Handler) {
		labels[pattern] = label
		r.Method(method, pattern, h)
		if method == http.MethodGet {
			r.Method(http.MethodHead, pattern, h) // fiber's app.Get served HEAD too
		}
	}

	r.Use(httpx.Prepare)
	r.Use(middleware.Recoverer)   // outer: turns a panic into fiber's 500
	r.Use(middleware.PanicLogger) // inner: logs it and re-panics; order per security.md s16
	r.Use(d.GlobalLimiter)
	r.Use(middleware.CORS)
	r.Use(middleware.NoSniff)
	r.Use(middleware.Favicon(d.FaviconFile))
	r.Use(metered(func(req *http.Request) string {
		if label, ok := labels[chi.RouteContext(req.Context()).RoutePattern()]; ok {
			return label
		}
		return "/"
	}))
	r.Use(websocketGate)
	r.Use(operatorGate)

	add(http.MethodGet, "/scalar.yaml", "/scalar.yaml", httpx.Handler(scalarYAML))
	add(http.MethodGet, "/health", "/health", httpx.Handler(d.Health.HealthCheck))
	add(http.MethodGet, "/metrics", "/metrics", generalAuth(observability.MetricsHandler()))
	add(http.MethodGet, "/ws", "/ws", httpx.Handler(d.WS.HandleWebSocket))
	add(http.MethodGet, "/monitor", "/monitor", generalAuth(httpx.Handler(d.WS.MonitorStats)))

	// /aws and /minio: operatorGate has already required the general token.
	add(http.MethodGet, "/aws/bucket-list", "/aws/bucket-list", httpx.Handler(d.AWS.BucketList))
	add(http.MethodGet, "/aws/{bucket}/exists", "/aws/:bucket/exists", httpx.Handler(d.AWS.BucketExists))
	add(http.MethodGet, "/aws/vault-list", "/aws/vault-list", httpx.Handler(d.AWS.GlacierVaultList))
	add(http.MethodPost, "/aws/glacier/{vault}/initiate-retrieval/{archiveId}", "/aws/glacier/:vault/initiate-retrieval/:archiveId", httpx.Handler(d.AWS.GlacierInitiateRetrieval))
	add(http.MethodGet, "/aws/glacier/{vault}/jobs", "/aws/glacier/:vault/jobs", httpx.Handler(d.AWS.GlacierListJobs))
	add(http.MethodGet, "/aws/glacier/{vault}/jobs/{jobId}/status", "/aws/glacier/:vault/jobs/:jobId/status", httpx.Handler(d.AWS.GlacierJobStatus))
	add(http.MethodGet, "/aws/glacier/{vault}/jobs/{jobId}/download", "/aws/glacier/:vault/jobs/:jobId/download", httpx.Handler(d.AWS.GlacierDownloadArchive))
	add(http.MethodPost, "/aws/glacier/{vault}/inventory", "/aws/glacier/:vault/inventory", httpx.Handler(d.AWS.GlacierInventoryRetrieval))
	add(http.MethodPost, "/aws/glacier/{vault}/jobs/{jobId}/async-download", "/aws/glacier/:vault/jobs/:jobId/async-download", httpx.Handler(d.AWS.GlacierInitiateAsyncDownload))
	add(http.MethodGet, "/aws/glacier/downloads/{downloadJobId}/status", "/aws/glacier/downloads/:downloadJobId/status", httpx.Handler(d.AWS.GlacierCheckDownloadStatus))

	add(http.MethodGet, "/minio/bucket-list", "/minio/bucket-list", httpx.Handler(d.Minio.BucketList))
	add(http.MethodGet, "/minio/{bucket}/exists", "/minio/:bucket/exists", httpx.Handler(d.Minio.BucketExists))
	add(http.MethodGet, "/minio/{bucket}/create", "/minio/:bucket/create", httpx.Handler(d.Minio.CreateBucket))
	add(http.MethodDelete, "/minio/{bucket}/delete", "/minio/:bucket/delete", httpx.Handler(d.Minio.RemoveBucket))

	// /resize feeds request bytes straight into ImageMagick, so it is not an
	// open compute surface. /archive lets a scoped token tier its own bucket only;
	// it is not gated by DISABLE_DELETE, the object stays readable at its URL.
	add(http.MethodPost, "/resize", "/resize", bucketAuth(httpx.Handler(d.Image.ResizeImage)))
	add(http.MethodPost, "/archive", "/archive", bucketAuth(httpx.Handler(d.Archive.ArchiveObjects)))

	// The w: and h: prefixes keep numeric path segments such as 2024/01 from
	// being read as sizes. fiber's "/:bucket/*" also matched "/bucket" with an
	// empty wildcard; chi needs that spelled out as its own pattern.
	if !d.DisableGet {
		get := httpx.Handler(d.Image.GetImage)
		add(http.MethodGet, "/{bucket}/w:{width}/h:{height}/*", "/:bucket/w::width/h::height/*", get)
		add(http.MethodGet, "/{bucket}/w:{width}/*", "/:bucket/w::width/*", get)
		add(http.MethodGet, "/{bucket}/h:{height}/*", "/:bucket/h::height/*", get)
		add(http.MethodGet, "/{bucket}/*", "/:bucket/*", get)
		add(http.MethodGet, "/{bucket}", "/:bucket/*", get)
	}
	if !d.DisableUpload {
		add(http.MethodDelete, "/batch/delete", "/batch/delete", bucketAuth(httpx.Handler(d.Image.BatchDelete)))
	}
	if !d.DisableDelete {
		del := bucketAuth(httpx.Handler(d.Image.DeleteImage))
		add(http.MethodDelete, "/{bucket}/*", "/:bucket/*", del)
		add(http.MethodDelete, "/{bucket}", "/:bucket/*", del)
	}

	// fiber's upload group was a Use on "/", so its limiter counted every
	// request that got this far: the upload routes, the index page and anything
	// unmatched. The shared Redis key with the global limiter is kept too.
	upload := func(h http.Handler) http.Handler { return h }
	if !d.DisableUpload {
		upload = d.UploadLimiter
		add(http.MethodPost, "/upload", "/upload", upload(bucketAuth(httpx.Handler(d.Image.UploadImage))))
		add(http.MethodPost, "/upload-url", "/upload-url", upload(bucketAuth(httpx.Handler(d.Image.UploadWithUrl))))
		add(http.MethodPost, "/batch/upload", "/batch/upload", upload(bucketAuth(httpx.Handler(d.Image.BatchUpload))))
	}
	add(http.MethodGet, "/", "/", upload(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		httpx.SendFile(w, req, "./public/scalar.html")
	})))

	unmatched := upload(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if allow := allowedMethods(r, req); allow != "" {
			w.Header().Set("Allow", allow)
			httpx.Text(w, http.StatusMethodNotAllowed, httpx.TextMethodNotAllowed)
			return
		}
		httpx.Text(w, http.StatusNotFound, "Cannot "+req.Method+" "+html.EscapeString(httpx.RawPath(req)))
	}))
	r.NotFound(unmatched.ServeHTTP)
	r.MethodNotAllowed(unmatched.ServeHTTP)

	return r
}

// allowedMethods lists, in fiber's order, the other methods that have a route
// for this path; empty means 404 rather than 405.
func allowedMethods(routes chi.Routes, req *http.Request) string {
	path := httpx.DetectionPath(httpx.RawPath(req))
	var allow []string
	for _, m := range fiberMethods {
		if m != req.Method && routes.Match(chi.NewRouteContext(), m, path) {
			allow = append(allow, m)
		}
	}
	return strings.Join(allow, ", ")
}

// metered counts every request but the ones fiber answered before its metrics
// middleware was registered: GET or HEAD of /health and /scalar.yaml.
func metered(label func(*http.Request) string) func(http.Handler) http.Handler {
	count := observability.PrometheusMiddleware(label)
	return func(next http.Handler) http.Handler {
		counted := count(next)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			path := httpx.DetectionPath(httpx.RawPath(req))
			if (req.Method == http.MethodGet || req.Method == http.MethodHead) && (path == "/health" || path == "/scalar.yaml") {
				next.ServeHTTP(w, req)
				return
			}
			counted.ServeHTTP(w, req)
		})
	}
}

// websocketGate is fiber's app.Use("/ws", ...): a plain prefix, every method.
// Browsers cannot set an Authorization header on a WebSocket, so the token comes
// from the query string and is compared in constant time.
func websocketGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !strings.HasPrefix(httpx.DetectionPath(httpx.RawPath(req)), "/ws") {
			next.ServeHTTP(w, req)
			return
		}
		if !handler.IsUpgrade(req) {
			httpx.Text(w, http.StatusUpgradeRequired, httpx.TextUpgradeRequired)
			return
		}
		if !service.TokenValid(httpx.Query(req, "token")) {
			audit.AuthFailure(req, service.ErrInvalidToken.Error())
			httpx.Text(w, http.StatusUnauthorized, httpx.TextUnauthorized)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// operatorGate is fiber's app.Group("/aws", auth) and app.Group("/minio", auth):
// plain prefixes, so "/minioextra/x.png" needs the general token as well.
func operatorGate(next http.Handler) http.Handler {
	gated := generalAuth(next)
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := httpx.DetectionPath(httpx.RawPath(req))
		if strings.HasPrefix(path, "/aws") || strings.HasPrefix(path, "/minio") {
			gated.ServeHTTP(w, req)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// generalAuth gates the operator routes: it accepts the general TOKEN only, so a
// bucket-scoped token can never reach an endpoint that acts on arbitrary buckets
// (list, create, remove) or exposes service-wide data.
//
// The 400 status is kept instead of being corrected to 401 so that clients see
// exactly the response they see today.
func generalAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := service.CheckToken(req); err != nil {
			// A valid bucket-scoped token is its own event: reaching an operator
			// route with one is almost always a misconfigured client, not an attack.
			// The extra resolve only runs on the failure path.
			if p, resolveErr := service.ResolvePrincipal(req); resolveErr == nil && p.Scoped {
				audit.ScopedTokenOnOperatorRoute(req, p.Bucket)
			} else {
				audit.AuthFailure(req, err.Error())
			}
			_ = service.Response(w, http.StatusBadRequest, false, err.Error(), nil)
			return
		}
		next.ServeHTTP(w, req)
	})
}

// bucketAuth gates the object write routes. It accepts the general TOKEN as well
// as a bucket-scoped token, and records the resolved principal so each handler
// can reconcile it with the bucket named in the request.
func bucketAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		p, err := service.ResolvePrincipal(req)
		if err != nil {
			audit.AuthFailure(req, err.Error())
			_ = service.Response(w, http.StatusBadRequest, false, err.Error(), nil)
			return
		}
		next.ServeHTTP(w, service.WithPrincipal(req, p))
	})
}

// scalarYAML serves the API description with APP_URL filled in.
func scalarYAML(w http.ResponseWriter, _ *http.Request) error {
	content, err := os.ReadFile("./public/scalar.yaml")
	if err != nil {
		httpx.JSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to read scalar file"})
		return nil
	}
	content = []byte(strings.ReplaceAll(string(content), "${APP_URL}", config.GetEnvOrDefault("APP_URL", "https://cdn.example.com")))
	httpx.Bytes(w, http.StatusOK, "text/yaml", content)
	return nil
}
