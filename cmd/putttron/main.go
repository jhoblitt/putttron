// putttron CLI: putting-strategy Monte Carlo on simulated greens.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "calibrate":
		cmdCalibrate(os.Args[2:])
	case "fit":
		cmdFit(os.Args[2:])
	case "sweep":
		cmdSweep(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: putttron <command> [flags]

commands:
  calibrate   flat-green make-%% by distance per skill (calibration gate)
  sweep       full parameter-matrix sweep for optimal rollout
`)
	os.Exit(2)
}

func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	return fs
}
