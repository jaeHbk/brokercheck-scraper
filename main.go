package main

import (
	"flag"
	"log"
	"os"
)

const (
	apiURL            = "https://api.brokercheck.finra.org/search/individual"
	pageSize          = 100
	apiPaginationCap  = 9000
	maxSubdivideDepth = 8
)

var (
	flagZipFile    = flag.String("zips", "uszips.csv", "Path to zip code CSV (header expected)")
	flagOutDir     = flag.String("out", ".", "Output directory")
	flagInitRadius = flag.Float64("radius", 25.0, "Initial search radius in miles")
	flagMinSpacing = flag.Float64("spacing", 40.0, "Minimum miles between representative zips. 0 disables.")
	flagDelayMs    = flag.Int("delay", 6000, "(deprecated; use --rps) Mean delay between requests in ms")
	flagRPS        = flag.Float64("rps", 0, "Global rate limit ceiling in requests/sec. 0 = derive from --delay (search default 0.17, enrich default 2.0).")
	flagMaxRetries = flag.Int("retries", 5, "Max retries on 429/5xx")
	flagLimit      = flag.Int("limit", 0, "Limit number of search points (0 = no limit). Smoke testing.")
	flagResume     = flag.Bool("resume", true, "Skip search points listed in progress.jsonl")
	flagWorkers    = flag.Int("workers", 1, "Concurrent workers (search default 1, enrich default 4).")
	flagDedupeOnly = flag.Bool("dedupe-only", false, "(deprecated alias for `dedupe` subcommand)")
	flagInput      = flag.String("input", "brokers_unique.json", "Source CRD list for `enrich` subcommand")
)

func main() {
	// Allow `<binary> <subcommand> <flags...>` by lifting a leading
	// non-flag arg out of os.Args before flag.Parse runs. Falling back
	// to flag.Arg(0) keeps the `<binary> <flags...> <subcommand>` form
	// working too.
	subcmd := "search"
	args := os.Args[1:]
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		subcmd = args[0]
		args = args[1:]
	}
	if err := flag.CommandLine.Parse(args); err != nil {
		log.Fatalf("flag parse: %v", err)
	}
	if subcmd == "search" && flag.NArg() >= 1 {
		subcmd = flag.Arg(0)
	}
	if *flagDedupeOnly {
		subcmd = "dedupe"
	}

	switch subcmd {
	case "search":
		runSearch()
	case "enrich":
		runEnrich()
	case "dedupe":
		log.Println("dedupe-only mode: reading brokers.jsonl")
		if err := dedupeStream(*flagOutDir); err != nil {
			log.Fatalf("dedupe failed: %v", err)
		}
		log.Println("done")
	default:
		log.Fatalf("unknown subcommand %q (expected search, enrich, or dedupe)", subcmd)
	}
}

// runSearch is implemented in stage_search.go.
// runEnrich is implemented in stage_enrich.go (Task 7).
