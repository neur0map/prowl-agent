package extract

import "testing"

func cxOf(r Result, name string) int {
	for _, s := range r.Symbols {
		if s.Name == name {
			return s.Complexity
		}
	}
	return -1
}

func TestComplexity(t *testing.T) {
	cases := []struct {
		lang, name, src string
		want            int
	}{
		// if + for + 2 switch cases + if = 5 decisions -> 6
		{"go", "f", "package m\nfunc f(x int) int {\n if x>0 { for i:=0;i<x;i++ { } }\n switch x { case 1: case 2: default: }\n if x>0 { }\n return x\n}\n", 6},
		// if + elif + for + while + except + case + ternary = 7 -> 8
		{"python", "f", "def f(x):\n if x>0:\n  for i in range(x): pass\n elif x<0: pass\n while x: x-=1\n try: pass\n except: pass\n match x:\n  case 1: pass\n return x if x else 0\n", 8},
		// if + for + switch_case(1) + catch + ternary = 5 -> 6
		{"typescript", "f", "function f(x:number){ if(x>0){ for(let i=0;i<x;i++){} } switch(x){ case 1: break; default: } try{}catch(e){} return x>0?1:2; }\n", 6},
		// if + for + while + 2 match arms + if = 6 -> 7
		{"rust", "f", "fn f(x:i32)->i32 { if x>0 { for _ in 0..x {} } while x>0 {} match x { 1=>{}, _=>{} } let _ = if x>0 {1} else {2}; x }\n", 7},
		// js: if + for + ternary = 3 -> 4 (guards the shared js/ts/tsx set)
		{"javascript", "f", "function f(x){ if(x>0){} for(let i=0;i<x;i++){} return x?1:2; }\n", 4},
		// cpp: if + for + case x2 (incl default) + catch = 5 -> 6
		{"cpp", "f", "int f(int x){ if(x>0){} for(int i=0;i<x;i++){} switch(x){case 1:break;default:break;} try{}catch(...){} return x; }\n", 6},
		// trivial function -> 1
		{"go", "g", "package m\nfunc g() int { return 1 }\n", 1},
	}
	for _, c := range cases {
		r := mustExtract(t, c.lang, c.src)
		if got := cxOf(r, c.name); got != c.want {
			t.Errorf("%s %s complexity = %d, want %d", c.lang, c.name, got, c.want)
		}
	}
}
