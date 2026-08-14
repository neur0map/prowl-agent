package extract

import "testing"

func TestSwiftExtract(t *testing.T) {
	src := []byte(`import SwiftUI
import Foundation

struct ServerStats: Codable {
    let cpu: Double
}

protocol StatsProvider {
    func fetch() async -> ServerStats
}

final class ServerManager: ObservableObject {
    @Published var stats: ServerStats?
    func refresh(for server: ServerInfo) { }
}

extension ServerStats {
    var cpuPercent: String { "x" }
}

enum ServerStatus { case online, offline }

struct DashboardView: View {
    @StateObject var manager = ServerManager()
    var body: some View {
        VStack {
            StatCardView(title: "CPU")
                .padding()
            GaugeCardView(label: "Memory")
                .background(Theme.surface)
        }
    }
}`)
	r, err := swiftExtractor{}.Extract(src)
	if err != nil {
		t.Fatal(err)
	}

	// A symbol is identified by (name, kind); ServerStats is legitimately both a
	// struct and an extension.
	have := map[string]bool{}
	for _, s := range r.Symbols {
		have[s.Name+"|"+s.Kind] = true
	}
	for _, want := range []string{
		"ServerStats|struct", "StatsProvider|protocol", "ServerManager|class",
		"ServerStatus|enum", "DashboardView|struct", "ServerStats|extension",
		"fetch|function", "refresh|function",
		// enum cases and type-level properties (model fields + view @State).
		"online|case", "offline|case",
		"cpu|property", "stats|property", "manager|property", "body|property",
	} {
		if !have[want] {
			t.Errorf("missing symbol %q; have=%v", want, have)
		}
	}

	includes, uses := map[string]bool{}, map[string]bool{}
	for _, e := range r.Edges {
		switch e.Kind {
		case "includes":
			includes[e.Raw] = true
		case "uses":
			uses[e.Raw] = true
		}
	}
	for _, m := range []string{"SwiftUI", "Foundation"} {
		if !includes[m] {
			t.Errorf("missing import edge %q; includes=%v", m, includes)
		}
	}
	// Project type references (resolve to declaring files later) and framework
	// types (stay informational) all surface as `uses`.
	for _, ty := range []string{"StatCardView", "GaugeCardView", "ServerManager", "ServerInfo", "ServerStats", "Theme"} {
		if !uses[ty] {
			t.Errorf("missing uses edge %q; uses=%v", ty, uses)
		}
	}
	// Lowercase calls (padding) must NOT become type-use edges.
	if uses["padding"] || uses["cpu"] {
		t.Errorf("lowercase identifier leaked into uses edges: %v", uses)
	}
}
