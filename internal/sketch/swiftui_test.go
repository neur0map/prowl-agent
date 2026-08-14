package sketch

import (
	"strings"
	"testing"
)

func TestSwiftUISketch(t *testing.T) {
	src := []byte(`import SwiftUI

struct DashboardView: View {
    @StateObject var manager = ServerManager()
    @State private var selected: Int = 0

    var body: some View {
        VStack(spacing: 12) {
            Text("Dashboard")
                .font(.headline)
                .foregroundColor(.primary)
            StatCardView(title: "CPU", value: 42)
                .padding()
            HStack {
                GaugeCardView(label: "Memory")
                Button("Refresh") { manager.refresh() }
            }
            if selected == 0 {
                ContainerListView()
            }
        }
        .padding(16)
        .background(Theme.surface)
        .onAppear { manager.start() }
    }
}`)
	sk, err := Of("Views/DashboardView.swift", src)
	if err != nil {
		t.Fatal(err)
	}
	out := sk.Text()
	t.Logf("\n%s", out)

	// Header names the view.
	if !strings.Contains(out, "DashboardView.swift") || !strings.Contains(out, "DashboardView") {
		t.Errorf("header missing view name:\n%s", out)
	}
	// Element tree: container + children (project + framework views alike).
	for _, want := range []string{"VStack", "Text", "StatCardView", "HStack", "GaugeCardView", "Button", "ContainerListView"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing view %q in sketch:\n%s", want, out)
		}
	}
	// Visual modifiers surface as props.
	for _, want := range []string{"padding", "background", "font"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing modifier %q:\n%s", want, out)
		}
	}
	// Handlers/actions are behavior, and the conditional is captured.
	if !strings.Contains(out, "onAppear") {
		t.Errorf("missing onAppear behavior:\n%s", out)
	}
	if !strings.Contains(out, "If") || !strings.Contains(out, "selected == 0") {
		t.Errorf("missing conditional:\n%s", out)
	}
	// The button's action is a behavior note, not a child view.
	if !strings.Contains(out, "manager.refresh()") {
		t.Errorf("missing button action behavior:\n%s", out)
	}
}
