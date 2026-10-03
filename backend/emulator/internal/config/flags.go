package config

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const usageExample = `Example:
  emulator -cabbers 1000 -interval 5s -duration 10m
`

// Parse reads the flags in args into a Profile. A flag.ErrHelp result means -h was given and the
// help text has been written to out; any other error is a rejected profile (exit code 2).
func Parse(args []string, out io.Writer) (Profile, error) {
	p := Default()
	area := formatArea(p.Area)
	var seed int64
	var runID string
	rampUp := time.Duration(-1)

	fs := flag.NewFlagSet("emulator", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.IntVar(&p.Cabbers, "cabbers", p.Cabbers, "number of cabbers in the park")
	fs.DurationVar(&p.Interval, "interval", p.Interval, "interval between two sends of one cabber")
	fs.DurationVar(&p.Duration, "duration", p.Duration, "run length; 0 runs until a signal")
	fs.DurationVar(&rampUp, "ramp-up", rampUp, "period the logins are spread over (default max(10s, cabbers*25ms))")
	fs.StringVar(&area, "area", area, "movement area: lat1,lon1,lat2,lon2")
	fs.StringVar(&p.Target, "target", p.Target, "base address of the gateway")
	fs.BoolVar(&p.AllowRemote, "allow-remote", false, "allow a target that is not a loopback address")
	fs.Int64Var(&seed, "seed", 0, "seed of routes and passwords (default random, printed at start)")
	fs.StringVar(&runID, "run-id", "", "run identifier used in emails (default random, printed at start)")
	fs.IntVar(&p.MaxConns, "max-conns", p.MaxConns, "ceiling of connections to the gateway")
	fs.IntVar(&p.Instances, "instances", p.Instances, "number of emulator processes sharing the park")
	fs.IntVar(&p.Instance, "instance", p.Instance, "index of this process, from 0")
	fs.StringVar(&p.ReportPath, "report", "", "summary file (default emulator-report-<run-id>.json)")
	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: emulator [flags]")
		fs.PrintDefaults()
		fmt.Fprint(out, usageExample)
	}
	if err := fs.Parse(args); err != nil {
		return Profile{}, err
	}
	if fs.NArg() > 0 {
		return Profile{}, &ValidationError{"arguments", "unexpected " + strconv.Quote(fs.Arg(0))}
	}

	parsed, err := parseArea(area)
	if err != nil {
		return Profile{}, err
	}
	p.Area = parsed

	if rampUp < 0 {
		rampUp = DefaultRampUp(p.Cabbers)
	}
	p.RampUp = rampUp

	if seed == 0 {
		seed = randomSeed()
	}
	p.Seed = seed
	if runID == "" {
		runID = randomRunID()
	}
	p.RunID = runID
	if p.ReportPath == "" {
		p.ReportPath = "emulator-report-" + p.RunID + ".json"
	}
	return p, p.Validate()
}

func formatArea(a Area) string {
	return fmt.Sprintf("%g,%g,%g,%g", a.MinLat, a.MinLon, a.MaxLat, a.MaxLon)
}

// parseArea reads lat1,lon1,lat2,lon2 and orders the corners so that either diagonal works.
func parseArea(s string) (Area, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return Area{}, &ValidationError{"area", "must be lat1,lon1,lat2,lon2"}
	}
	var v [4]float64
	for i, part := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return Area{}, &ValidationError{"area", "must be lat1,lon1,lat2,lon2 of numbers"}
		}
		v[i] = f
	}
	a := Area{MinLat: v[0], MinLon: v[1], MaxLat: v[2], MaxLon: v[3]}
	if a.MinLat > a.MaxLat {
		a.MinLat, a.MaxLat = a.MaxLat, a.MinLat
	}
	if a.MinLon > a.MaxLon {
		a.MinLon, a.MaxLon = a.MaxLon, a.MinLon
	}
	return a, nil
}

func randomSeed() int64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	seed := int64(binary.BigEndian.Uint64(b[:]) >> 1)
	if seed == 0 {
		seed = 1
	}
	return seed
}

const runIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

func randomRunID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	for i := range b {
		b[i] = runIDAlphabet[int(b[i])%len(runIDAlphabet)]
	}
	return string(b[:])
}
