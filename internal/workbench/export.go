package workbench

import (
	"bytes"
	"context"
	"errors"
	"html/template"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/prowl-agent/prowl-agent/internal/jobs"
)

const MaxOfflineExportBytes = 256 << 10
const MaxSynchronousOfflineExportBytes = 64 << 10

const offlineExportCSP = "default-src 'none'; connect-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'"

var ErrOfflineExportTooLarge = errors.New("offline export exceeds response bound")

type OfflineExport struct {
	HTML            []byte
	GeneratedAt     time.Time
	ResourceVersion string
}

type offlineExportDocument struct {
	Workspace       string
	GeneratedAt     string
	ResourceVersion string
	APIVersion      string
	Freshness       Freshness
	Files           int
	Symbols         int
	Edges           int
	Resources       int
	Entrypoints     []string
	Documents       []string
	Languages       []offlineExportCount
	Capabilities    []offlineExportCapability
}

type offlineExportRequest struct {
	Export *OfflineExport
	Job    *jobs.Job
}

type offlineExportCount struct {
	Name  string
	Count int
}

type offlineExportCapability struct {
	Name        string
	Title       string
	Description string
	Privacy     string
	Version     string
}

var offlineExportTemplate = template.Must(template.New("offline-export").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; connect-src 'none'; script-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'">
<meta name="generated-at" content="{{.GeneratedAt}}">
<meta name="resource-version" content="{{.ResourceVersion}}">
<meta name="api-version" content="{{.APIVersion}}">
<title>Prowl offline project snapshot - {{.Workspace}}</title>
<style>
:root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; background: #08090a; color: #f7f8f8; }
body { margin: 0; min-width: 320px; background: #08090a; }
main { max-width: 960px; margin: 0 auto; padding: 48px 28px 72px; }
header, section { border-bottom: 1px solid rgba(255,255,255,.12); padding: 0 0 28px; margin: 0 0 28px; }
h1 { margin: 0; font-size: clamp(28px, 5vw, 48px); line-height: 1.05; }
h2 { margin: 0 0 12px; font-size: 16px; letter-spacing: .02em; }
p, li, dd { color: #d0d6e0; line-height: 1.55; } .eyebrow { color: #7170ff; font-size: 12px; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
dl { display: grid; grid-template-columns: max-content 1fr; gap: 8px 20px; margin: 24px 0 0; } dt { color: #a8af9b; } dd { margin: 0; font-family: ui-monospace, monospace; overflow-wrap: anywhere; }
.grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 12px; padding: 0; list-style: none; } .card { border: 1px solid rgba(255,255,255,.12); border-radius: 8px; padding: 14px; } .value { display: block; font: 700 24px/1 ui-monospace, monospace; color: #f7f8f8; }
ul { padding-left: 20px; } code { color: #b9d9ff; } footer { color: #a8af9b; font-size: 13px; }
@media (max-width: 540px) { main { padding: 28px 20px 48px; } dl { grid-template-columns: 1fr; gap: 2px; } }
</style>
</head>
<body>
<main>
<header>
<p class="eyebrow">Prowl offline project snapshot</p>
<h1>{{.Workspace}}</h1>
<p>Read-only, source-backed project evidence captured for offline review.</p>
<dl>
<dt>Generated at</dt><dd>{{.GeneratedAt}}</dd>
<dt>Source version</dt><dd>{{.ResourceVersion}}</dd>
<dt>API version</dt><dd>{{.APIVersion}}</dd>
<dt>Index freshness</dt><dd>{{.Freshness.Status}}{{if .Freshness.LastIndexed}} · {{.Freshness.LastIndexed}}{{end}}</dd>
</dl>
</header>
<section aria-labelledby="overview-heading">
<h2 id="overview-heading">Indexed evidence</h2>
<ul class="grid">
<li class="card"><span class="value">{{.Files}}</span> files</li>
<li class="card"><span class="value">{{.Symbols}}</span> symbols</li>
<li class="card"><span class="value">{{.Edges}}</span> edges</li>
<li class="card"><span class="value">{{.Resources}}</span> resources</li>
</ul>
</section>
{{if .Entrypoints}}<section aria-labelledby="entrypoints-heading"><h2 id="entrypoints-heading">Entrypoints</h2><ul>{{range .Entrypoints}}<li><code>{{.}}</code></li>{{end}}</ul></section>{{end}}
{{if .Documents}}<section aria-labelledby="documents-heading"><h2 id="documents-heading">Guide documents</h2><ul>{{range .Documents}}<li><code>{{.}}</code></li>{{end}}</ul></section>{{end}}
{{if .Languages}}<section aria-labelledby="languages-heading"><h2 id="languages-heading">Languages</h2><ul class="grid">{{range .Languages}}<li class="card"><span class="value">{{.Count}}</span>{{.Name}}</li>{{end}}</ul></section>{{end}}
{{if .Capabilities}}<section aria-labelledby="capabilities-heading"><h2 id="capabilities-heading">Available capabilities</h2><ul>{{range .Capabilities}}<li><strong>{{.Title}}</strong> (<code>{{.Name}}</code>, {{.Version}}) - {{.Description}} <span class="eyebrow">{{.Privacy}}</span></li>{{end}}</ul></section>{{end}}
<footer>Provenance: Prowl Workbench generated this bounded snapshot from the indexed project projection. It makes no network requests and contains no live credentials.</footer>
</main>
</body>
</html>
`))

// OfflineExport renders a self-contained, read-only snapshot from the same
// bounded projection served by the authenticated workbench API.
func (service *Service) OfflineExport(ctx context.Context) (OfflineExport, error) {
	brief, err := service.Brief(ctx)
	if err != nil {
		return OfflineExport{}, err
	}
	generatedAt := time.Now().UTC()
	if service.exportNow != nil {
		generatedAt = service.exportNow().UTC()
	}
	if generatedAt.IsZero() {
		return OfflineExport{}, ErrInvalidDerivedData
	}
	document := offlineExportDocument{
		Workspace:       brief.Workspace.Name,
		GeneratedAt:     generatedAt.Format(time.RFC3339),
		ResourceVersion: brief.resourceVersion,
		APIVersion:      APIVersion,
		Freshness:       brief.Freshness,
		Files:           brief.Overview.Counts.Files,
		Symbols:         brief.Overview.Counts.Symbols,
		Edges:           brief.Overview.Counts.Edges,
		Resources:       brief.Overview.Counts.Resources,
		Entrypoints:     append([]string(nil), brief.Overview.Entrypoints...),
		Documents:       append([]string(nil), brief.Overview.Docs...),
		Languages:       sortedOfflineExportCounts(brief.Overview.Counts.Langs),
		Capabilities:    offlineExportCapabilities(brief),
	}
	var output bytes.Buffer
	if err := offlineExportTemplate.Execute(&output, document); err != nil {
		return OfflineExport{}, err
	}
	if output.Len() == 0 || output.Len() > MaxOfflineExportBytes {
		return OfflineExport{}, ErrOfflineExportTooLarge
	}
	return OfflineExport{HTML: output.Bytes(), GeneratedAt: generatedAt, ResourceVersion: brief.resourceVersion}, nil
}

func sortedOfflineExportCounts(values map[string]int) []offlineExportCount {
	output := make([]offlineExportCount, 0, len(values))
	for name, count := range values {
		output = append(output, offlineExportCount{Name: name, Count: count})
	}
	sort.Slice(output, func(i, j int) bool { return output[i].Name < output[j].Name })
	return output
}

func offlineExportCapabilities(brief Brief) []offlineExportCapability {
	output := make([]offlineExportCapability, 0, len(brief.Capabilities))
	for _, capability := range brief.Capabilities {
		output = append(output, offlineExportCapability{
			Name:        capability.Name,
			Title:       capability.Title,
			Description: capability.Description,
			Privacy:     capability.Privacy,
			Version:     capability.Version,
		})
	}
	return output
}

func (service *Service) requestOfflineExport(ctx context.Context) (offlineExportRequest, error) {
	export, err := service.OfflineExport(ctx)
	if err != nil {
		return offlineExportRequest{}, err
	}
	limit := MaxSynchronousOfflineExportBytes
	if service.maxSynchronousExportBytes > 0 {
		limit = service.maxSynchronousExportBytes
	}
	if len(export.HTML) <= limit {
		return offlineExportRequest{Export: &export}, nil
	}
	live, err := service.liveOperations()
	if err != nil {
		return offlineExportRequest{}, err
	}
	job, _, err := live.jobs.EnqueueOrResumeExport(ctx)
	if err != nil {
		return offlineExportRequest{}, err
	}
	return offlineExportRequest{Job: &job}, nil
}

func (service *Service) runOfflineExport(ctx context.Context, job jobs.Job, progress func(string, int) error) error {
	if job.Kind != jobs.KindExport {
		return jobs.ErrInvalidJob
	}
	if err := progress("rendering", 20); err != nil {
		return err
	}
	export, err := service.OfflineExport(ctx)
	if err != nil {
		return err
	}
	if err := progress("writing", 80); err != nil {
		return err
	}
	live, err := service.liveOperations()
	if err != nil {
		return err
	}
	if err := live.jobs.WriteExportArtifact(ctx, job.ID, export.HTML); err != nil {
		return err
	}
	return progress("finalizing", 99)
}
func serveOfflineExport(response http.ResponseWriter, request *http.Request, service *Service) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if request.Body != nil && request.Body != http.NoBody {
		reader := http.MaxBytesReader(response, request.Body, 1)
		var byteRead [1]byte
		if count, err := reader.Read(byteRead[:]); count != 0 || err != io.EOF {
			writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
			return
		}
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	result, err := service.requestOfflineExport(request.Context())
	if err != nil {
		status, code, message := projectionError(err, "export")
		writeError(response, request, status, code, message, errorResourceVersion(err))
		return
	}
	if result.Job != nil {
		live, err := service.liveOperations()
		if err != nil {
			writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
			return
		}
		job, state, err := live.jobs.Snapshot(request.Context(), result.Job.ID)
		if err != nil {
			writeJobError(response, request, err)
			return
		}
		jobResponse, err := newJobResponse(job, state)
		if err != nil {
			writeError(response, request, http.StatusServiceUnavailable, "job_unavailable", "job is unavailable", unavailableVersion)
			return
		}
		writeJobSuccess(response, request, http.StatusAccepted, jobResponse)
		return
	}
	writeOfflineExportHTML(response, request, result.Export.HTML)
}

func serveOfflineExportArtifact(response http.ResponseWriter, request *http.Request, service *Service, id string) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, request, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed", unavailableVersion)
		return
	}
	if !validJobID(id) {
		writeError(response, request, http.StatusBadRequest, "invalid_request", "request is invalid", unavailableVersion)
		return
	}
	if service == nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "workbench service is unavailable", unavailableVersion)
		return
	}
	live, err := service.liveOperations()
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "service_unavailable", "service is unavailable", unavailableVersion)
		return
	}
	job, state, err := live.jobs.Snapshot(request.Context(), id)
	if err != nil {
		if errors.Is(err, jobs.ErrUnknownJob) {
			writeError(response, request, http.StatusNotFound, "export_not_found", "export was not found", unavailableVersion)
			return
		}
		writeJobError(response, request, err)
		return
	}
	if job.Kind != jobs.KindExport {
		writeError(response, request, http.StatusNotFound, "export_not_found", "export was not found", unavailableVersion)
		return
	}
	if !job.Terminal() {
		jobResponse, err := newJobResponse(job, state)
		if err != nil {
			writeError(response, request, http.StatusServiceUnavailable, "job_unavailable", "job is unavailable", unavailableVersion)
			return
		}
		writeJobSuccess(response, request, http.StatusAccepted, jobResponse)
		return
	}
	if job.Status != jobs.StatusSucceeded {
		writeError(response, request, http.StatusConflict, "export_unavailable", "export is unavailable", unavailableVersion)
		return
	}
	content, err := live.jobs.ReadExportArtifact(request.Context(), id)
	if err != nil {
		writeError(response, request, http.StatusServiceUnavailable, "export_unavailable", "export is unavailable", unavailableVersion)
		return
	}
	writeOfflineExportHTML(response, request, content)
}

func writeOfflineExportHTML(response http.ResponseWriter, request *http.Request, content []byte) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.Header().Set("Content-Disposition", `attachment; filename="prowl-workbench.html"`)
	response.Header().Set("Content-Security-Policy", offlineExportCSP)
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Request-ID", responseRequestID(request))
	response.WriteHeader(http.StatusOK)
	_, _ = response.Write(content)
}
