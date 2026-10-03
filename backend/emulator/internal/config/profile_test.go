package config

import (
	"errors"
	"flag"
	"strings"
	"testing"
	"time"
)

func TestDefaultProfileIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default profile rejected: %v", err)
	}
}

func TestValidateRejectsEachBoundary(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Profile)
		field  string
	}{
		{"zero cabbers", func(p *Profile) { p.Cabbers = 0 }, "cabbers"},
		{"negative cabbers", func(p *Profile) { p.Cabbers = -5 }, "cabbers"},
		{"too many cabbers", func(p *Profile) { p.Cabbers = MaxCabbers + 1 }, "cabbers"},
		{"zero interval", func(p *Profile) { p.Interval = 0 }, "interval"},
		{"interval below minimum", func(p *Profile) { p.Interval = 99 * time.Millisecond }, "interval"},
		{"negative duration", func(p *Profile) { p.Duration = -time.Second }, "duration"},
		{"negative ramp-up", func(p *Profile) { p.RampUp = -time.Second }, "ramp-up"},
		{"zero max-conns", func(p *Profile) { p.MaxConns = 0 }, "max-conns"},
		{"zero instances", func(p *Profile) { p.Instances = 0 }, "instances"},
		{"instance out of range", func(p *Profile) { p.Instances, p.Instance = 2, 2 }, "instance"},
		{"negative instance", func(p *Profile) { p.Instance = -1 }, "instance"},
		{"latitude out of range", func(p *Profile) { p.Area = Area{MinLat: 10, MinLon: 10, MaxLat: 91, MaxLon: 11} }, "area"},
		{"longitude out of range", func(p *Profile) { p.Area = Area{MinLat: 10, MinLon: 10, MaxLat: 11, MaxLon: 181} }, "area"},
		{"degenerate area", func(p *Profile) { p.Area = Area{MinLat: 10, MinLon: 10, MaxLat: 10, MaxLon: 11} }, "area"},
		{"too narrow area", func(p *Profile) { p.Area = Area{MinLat: 55, MinLon: 37, MaxLat: 55.001, MaxLon: 38} }, "area"},
		{"bad target", func(p *Profile) { p.Target = "not a url" }, "target"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Default()
			tc.mutate(&p)
			err := p.Validate()
			var verr *ValidationError
			if !errors.As(err, &verr) || verr.Field != tc.field {
				t.Fatalf("Validate() = %v, want a ValidationError on %q", err, tc.field)
			}
		})
	}
}

func TestValidateAcceptsLimits(t *testing.T) {
	for _, cabbers := range []int{1, MaxCabbers} {
		p := Default()
		p.Cabbers = cabbers
		p.Interval = MinInterval
		if err := p.Validate(); err != nil {
			t.Errorf("cabbers=%d: %v", cabbers, err)
		}
	}
}

func TestDefaultRampUp(t *testing.T) {
	cases := map[int]time.Duration{
		1:     10 * time.Second,
		100:   10 * time.Second,
		400:   10 * time.Second,
		1000:  25 * time.Second,
		50000: 1250 * time.Second,
	}
	for cabbers, want := range cases {
		if got := DefaultRampUp(cabbers); got != want {
			t.Errorf("DefaultRampUp(%d) = %s, want %s", cabbers, got, want)
		}
	}
}

func TestParseDefaultsAndDerivedRampUp(t *testing.T) {
	var out strings.Builder
	p, err := Parse([]string{"-cabbers", "1000"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if p.RampUp != 25*time.Second {
		t.Errorf("ramp-up = %s, want derived 25s", p.RampUp)
	}
	if p.Seed == 0 || len(p.RunID) != 8 {
		t.Errorf("seed %d and run id %q must be generated", p.Seed, p.RunID)
	}
	if p.ReportPath != "emulator-report-"+p.RunID+".json" {
		t.Errorf("report path = %q", p.ReportPath)
	}
}

func TestParseExplicitRampUpWins(t *testing.T) {
	p, err := Parse([]string{"-cabbers", "1000", "-ramp-up", "3s"}, &strings.Builder{})
	if err != nil || p.RampUp != 3*time.Second {
		t.Fatalf("ramp-up = %s, err = %v", p.RampUp, err)
	}
}

func TestParseRejectsInvalidProfile(t *testing.T) {
	for _, args := range [][]string{
		{"-cabbers", "0"},
		{"-cabbers", "-3"},
		{"-interval", "0s"},
		{"-area", "1,2,3"},
		{"-area", "a,b,c,d"},
		{"stray"},
	} {
		if _, err := Parse(args, &strings.Builder{}); err == nil {
			t.Errorf("Parse(%v) accepted", args)
		}
	}
}

func TestParseAreaOrdersCorners(t *testing.T) {
	p, err := Parse([]string{"-area", "55.9,37.85,55.6,37.4"}, &strings.Builder{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Area != DefaultArea {
		t.Errorf("area = %+v, want %+v", p.Area, DefaultArea)
	}
}

func TestParseHelpListsDefaultsAndExample(t *testing.T) {
	var out strings.Builder
	_, err := Parse([]string{"-h"}, &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	for _, want := range []string{"-cabbers", "-interval", "-allow-remote", "(default 100)", "Example:"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help lacks %q:\n%s", want, out.String())
		}
	}
}
