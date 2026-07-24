package workbench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExploreProjectsDeterministicSectionsAndTours(t *testing.T) {
	project := openWorkbenchProject(t, map[string]string{
		"README.md":                "# Example\n",
		"ARCHITECTURE.md":          "# Architecture\n",
		"cmd/server/main.go":       "package main\n\nimport \"example/internal/auth\"\n\nfunc main() { auth.Run() }\n",
		"internal/auth/service.go": "package auth\n\nfunc Run() {}\n",
		"internal/auth/token.go":   "package auth\n\nfunc Validate() {}\n",
	})
	service, err := NewService(project)
	if err != nil {
		t.Fatal(err)
	}

	explore, err := service.Explore(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if explore.Workspace.Name == "" || len(explore.Sections) != 5 {
		t.Fatalf("explore=%+v", explore)
	}
	wantSections := []string{"guides", "entrypoints", "subsystems", "hotspots", "capabilities"}
	for index, want := range wantSections {
		if explore.Sections[index].ID != want {
			t.Fatalf("section[%d]=%q want %q", index, explore.Sections[index].ID, want)
		}
	}
	if len(explore.Tours) != 3 {
		t.Fatalf("tour summaries=%+v", explore.Tours)
	}
	for _, summary := range explore.Tours {
		if summary.Steps < 5 || summary.Steps > 12 {
			t.Fatalf("tour %q steps=%d", summary.ID, summary.Steps)
		}
		tour, err := service.GuidedTour(context.Background(), summary.ID)
		if err != nil {
			t.Fatalf("tour %q: %v", summary.ID, err)
		}
		if len(tour.Steps) != summary.Steps {
			t.Fatalf("tour %q=%+v", summary.ID, tour)
		}
	}
	encoded, err := json.Marshal(explore)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), project.Workspace.Root) {
		t.Fatalf("explore leaked workspace root: %s", encoded)
	}
}

func TestGuidedTourRejectsUnknownID(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{"main.go": "package main\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.GuidedTour(context.Background(), "../../secret"); err == nil {
		t.Fatal("traversing tour ID was accepted")
	}
	if _, err := service.GuidedTour(context.Background(), "unknown"); err == nil {
		t.Fatal("unknown tour ID was accepted")
	}
}

func TestExploreAndTourAPIRoutesRequireBoundedReadRequests(t *testing.T) {
	service, err := NewService(openWorkbenchProject(t, map[string]string{"README.md": "# Example\n", "cmd/server/main.go": "package main\nfunc main() {}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewAPI(APIOptions{Bootstrap: testBootstrap(t), AllowedOrigin: "http://127.0.0.1:43117", Service: service})
	if err != nil {
		t.Fatal(err)
	}

	for _, check := range []struct {
		path, wantCode string
		status         int
	}{
		{path: "/api/v1/explore", wantCode: "", status: http.StatusOK},
		{path: "/api/v1/tours/onboarding", wantCode: "", status: http.StatusOK},
		{path: "/api/v1/tours/unknown", wantCode: "not_found", status: http.StatusNotFound},
		{path: "/api/v1/tours/%2e%2e%2fsecret", wantCode: "not_found", status: http.StatusNotFound},
	} {
		t.Run(check.path, func(t *testing.T) {
			request := authorizedAPIRequest(check.path, "explore-request")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != check.status {
				t.Fatalf("status=%d want=%d body=%q", response.Code, check.status, response.Body.String())
			}
			if check.wantCode != "" && !strings.Contains(response.Body.String(), `"code":"`+check.wantCode+`"`) {
				t.Fatalf("body=%q", response.Body.String())
			}
		})
	}
}
