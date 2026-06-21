package extract

import "testing"

func TestElixirExtractor(t *testing.T) {
	src := `defmodule MyApp.Accounts.User do
  alias MyApp.Repo
  alias MyApp.Accounts.{Profile, Role}
  import Ecto.Query
  use MyApp.Web, :model

  def greet(name) do
    case name do
      "" -> "stranger"
      n -> n
    end
  end

  defp normalize(x), do: x

  defmacro mac(x), do: x
end
`
	r := mustExtract(t, "elixir", src)

	if !has(symNames(r, "module"), "MyApp.Accounts.User") {
		t.Fatalf("modules=%v want MyApp.Accounts.User", symNames(r, "module"))
	}
	for _, fn := range []string{"greet", "normalize", "mac"} {
		if !has(symNames(r, "function"), fn) {
			t.Fatalf("functions=%v want %s", symNames(r, "function"), fn)
		}
	}
	for _, imp := range []string{"MyApp.Repo", "MyApp.Accounts.Profile", "MyApp.Accounts.Role", "Ecto.Query", "MyApp.Web"} {
		if !has(edgeRaws(r, "includes"), imp) {
			t.Fatalf("imports=%v want %s", edgeRaws(r, "includes"), imp)
		}
	}
	// The module name is recorded as a namespace resource so alias/import/use of
	// it resolve to this file.
	var sawNS bool
	for _, res := range r.Resources {
		if res.Kind == "namespace" && res.Name == "MyApp.Accounts.User" {
			sawNS = true
		}
	}
	if !sawNS {
		t.Errorf("resources=%v want namespace MyApp.Accounts.User", r.Resources)
	}
	// greet's case has two -> arms -> complexity 3.
	for _, s := range r.Symbols {
		if s.Name == "greet" && s.Complexity != 3 {
			t.Errorf("greet complexity = %d, want 3", s.Complexity)
		}
	}
}
