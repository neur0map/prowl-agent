package extract

import "testing"

func TestGoExtractor(t *testing.T) {
	src := "package srv\n\n" +
		"import (\n\t\"fmt\"\n\t\"github.com/x/y/store\"\n)\n\n" +
		"type Server struct{ name string }\n\n" +
		"func New() *Server { return &Server{} }\n\n" +
		"func (s *Server) Run() error { fmt.Println(s.name); return nil }\n"
	r := mustExtract(t, "go", src)

	if !has(symNames(r, "type"), "Server") {
		t.Fatalf("types=%v want Server", symNames(r, "type"))
	}
	if !has(symNames(r, "function"), "New") {
		t.Fatalf("functions=%v want New", symNames(r, "function"))
	}
	if !has(symNames(r, "method"), "Run") {
		t.Fatalf("methods=%v want Run", symNames(r, "method"))
	}
	inc := edgeRaws(r, "includes")
	if !has(inc, "fmt") || !has(inc, "github.com/x/y/store") {
		t.Fatalf("imports=%v want fmt and github.com/x/y/store", inc)
	}
}
