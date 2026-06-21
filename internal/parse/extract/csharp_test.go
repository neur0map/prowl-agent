package extract

import "testing"

func TestCsharpExtractor(t *testing.T) {
	src := "using System;\n" +
		"using App.Services;\n" +
		"namespace App {\n" +
		"  public interface IRun { void Go(); }\n" +
		"  public enum State { On, Off }\n" +
		"  public struct Point { public int X; }\n" +
		"  public class Service {\n" +
		"    public int Handle(int x) { if (x > 0) { for (int i = 0; i < x; i++) {} } return x > 0 ? 1 : 2; }\n" +
		"  }\n" +
		"}\n"
	r := mustExtract(t, "csharp", src)

	if !has(symNames(r, "class"), "Service") {
		t.Fatalf("classes=%v want Service", symNames(r, "class"))
	}
	if !has(symNames(r, "interface"), "IRun") {
		t.Fatalf("interfaces=%v want IRun", symNames(r, "interface"))
	}
	if !has(symNames(r, "enum"), "State") {
		t.Fatalf("enums=%v want State", symNames(r, "enum"))
	}
	if !has(symNames(r, "struct"), "Point") {
		t.Fatalf("structs=%v want Point", symNames(r, "struct"))
	}
	if !has(symNames(r, "method"), "Handle") {
		t.Fatalf("methods=%v want Handle", symNames(r, "method"))
	}
	if !has(edgeRaws(r, "includes"), "App.Services") {
		t.Fatalf("usings=%v want App.Services", edgeRaws(r, "includes"))
	}
	// Handle: if + for + ternary = 3 decisions -> complexity 4.
	for _, s := range r.Symbols {
		if s.Name == "Handle" && s.Complexity != 4 {
			t.Errorf("Handle complexity = %d, want 4", s.Complexity)
		}
	}
}
