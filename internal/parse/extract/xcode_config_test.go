package extract

import "testing"

func symSet(r Result) map[string]string {
	m := map[string]string{}
	for _, s := range r.Symbols {
		m[s.Name] = s.Kind
	}
	return m
}

func TestPlistExtract(t *testing.T) {
	src := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key>
	<string>com.neur0map.deskmon</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSAppTransportSecurity</key>
	<dict><key>NSAllowsLocalNetworking</key><true/></dict>
</dict>
</plist>`)
	r, _ := plistExtractor{}.Extract(src)
	set := symSet(r)
	for _, k := range []string{"CFBundleIdentifier", "LSUIElement", "NSAppTransportSecurity", "NSAllowsLocalNetworking"} {
		if set[k] != "setting" {
			t.Errorf("plist key %q kind=%q, want setting", k, set[k])
		}
	}
}

func TestPbxprojExtract(t *testing.T) {
	src := []byte(`/* Begin PBXNativeTarget section */
		A1 /* deskmon */ = {
			isa = PBXNativeTarget;
			productName = deskmon;
			buildSettings = {
				PRODUCT_BUNDLE_IDENTIFIER = com.neur0map.deskmon;
				SWIFT_VERSION = 6.0;
				fileRef = A2;
			};
		};`)
	r, _ := pbxprojExtractor{}.Extract(src)
	set := symSet(r)
	if set["deskmon"] != "target" {
		t.Errorf("target deskmon kind=%q, want target", set["deskmon"])
	}
	for _, k := range []string{"PRODUCT_BUNDLE_IDENTIFIER", "SWIFT_VERSION"} {
		if set[k] != "setting" {
			t.Errorf("build setting %q kind=%q, want setting", k, set[k])
		}
	}
	if _, ok := set["fileRef"]; ok {
		t.Errorf("lowercase pbxproj key leaked as symbol: %v", set)
	}
}

func TestXcconfigExtract(t *testing.T) {
	src := []byte(`#include "Base.xcconfig"
// a comment
PRODUCT_NAME = deskmon
MARKETING_VERSION = 1.0`)
	r, _ := xcconfigExtractor{}.Extract(src)
	set := symSet(r)
	for _, k := range []string{"PRODUCT_NAME", "MARKETING_VERSION"} {
		if set[k] != "setting" {
			t.Errorf("xcconfig key %q kind=%q, want setting", k, set[k])
		}
	}
	hasInc := false
	for _, e := range r.Edges {
		if e.Kind == "includes" && e.Raw == "Base.xcconfig" {
			hasInc = true
		}
	}
	if !hasInc {
		t.Errorf("missing #include edge; edges=%+v", r.Edges)
	}
}
