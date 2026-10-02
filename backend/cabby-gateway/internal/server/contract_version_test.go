package server

import (
	"regexp"
	"testing"

	"github.com/Keane81/Cabby/backend/cabby-gateway/api"
)

func TestContractVersionIsSemver(t *testing.T) {
	versionRE := regexp.MustCompile(`(?m)^[ \t]+version:[ \t]+(\S+)[ \t]*$`)
	semverRE := regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

	match := versionRE.FindStringSubmatch(string(api.OpenAPIDocument))
	if match == nil {
		t.Fatal("contract has no info.version")
	}
	version := match[1]
	if version != "1.2.0" {
		t.Fatalf("info.version = %q, want 1.2.0", version)
	}
	if !semverRE.MatchString(version) {
		t.Fatalf("info.version %q is not valid SemVer", version)
	}
}
