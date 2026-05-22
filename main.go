package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Response types ---

type BrokerResponse struct {
	ErrorCode    int     `json:"errorCode"`
	ErrorMessage string  `json:"errorMessage"`
	Hits         HitData `json:"hits"`
}

type HitData struct {
	Total int         `json:"total"`
	Hits  []BrokerHit `json:"hits"`
}

type BrokerHit struct {
	Source BrokerSource `json:"_source"`
}

type BrokerSource struct {
	CRD                string       `json:"ind_source_id"`
	FirstName          string       `json:"ind_firstname"`
	LastName           string       `json:"ind_lastname"`
	CurrentEmployments []Employment `json:"ind_current_employments"`
}

type Employment struct {
	FirmName string `json:"firm_name"`
	City     string `json:"branch_city"`
	State    string `json:"branch_state"`
	Zip      string `json:"branch_zip"`
}

// --- Domain types ---

type ZipLocation struct {
	Zip  string
	Lat  float64
	Lon  float64
	City string
}

// SearchPoint is a (lat,lon,radius) tuple — either an original zip
// or a subdivision generated when an area exceeds the API cap.
type SearchPoint struct {
	ID     string
	Lat    float64
	Lon    float64
	Radius float64
	Depth  int
}

const (
	apiURL            = "https://api.brokercheck.finra.org/search/individual"
	pageSize          = 100
	apiPaginationCap  = 9000 // API rejects start+nrows > 9000
	maxSubdivideDepth = 8
)

var (
	flagZipFile    = flag.String("zips", "uszips.csv", "Path to zip code CSV (header expected)")
	flagOutDir     = flag.String("out", ".", "Output directory")
	flagInitRadius = flag.Float64("radius", 25.0, "Initial search radius in miles")
	flagMinSpacing = flag.Float64("spacing", 40.0, "Minimum miles between representative zips (spatial dedup). 0 disables.")
	flagDelayMs    = flag.Int("delay", 6000, "Mean delay between requests in ms (jittered ±50%)")
	flagMaxRetries = flag.Int("retries", 5, "Max retries on 429/5xx")
	flagLimit      = flag.Int("limit", 0, "Limit number of search points (0 = no limit). For smoke testing.")
	flagResume     = flag.Bool("resume", true, "Skip search points listed in progress.jsonl")
	flagWorkers    = flag.Int("workers", 1, "Concurrent workers. Keep at 1 for single-IP politeness.")
	flagDedupeOnly = flag.Bool("dedupe-only", false, "Skip scraping; just dedupe existing brokers.jsonl into brokers_unique.{json,csv}")
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	userAgents = []string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:125.0) Gecko/20100101 Firefox/125.0",
	}
	rng   = rand.New(rand.NewSource(time.Now().UnixNano()))
	rngMu sync.Mutex
)

// --- Geo helpers ---

func haversineMiles(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusMi = 3958.7613
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMi * c
}

func offsetMiles(lat, lon, miles, bearingDeg float64) (float64, float64) {
	const earthRadiusMi = 3958.7613
	br := bearingDeg * math.Pi / 180
	d := miles / earthRadiusMi
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180
	lat2 := math.Asin(math.Sin(lat1)*math.Cos(d) + math.Cos(lat1)*math.Sin(d)*math.Cos(br))
	lon2 := lon1 + math.Atan2(math.Sin(br)*math.Sin(d)*math.Cos(lat1),
		math.Cos(d)-math.Sin(lat1)*math.Sin(lat2))
	return lat2 * 180 / math.Pi, lon2 * 180 / math.Pi
}

// --- Zip loading + spatial dedup ---

func loadZipCodes(filename string) ([]ZipLocation, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	col := func(name string) int {
		for i, h := range header {
			if strings.EqualFold(h, name) {
				return i
			}
		}
		return -1
	}
	zipIdx, latIdx, lonIdx, cityIdx := col("zip"), col("lat"), col("lng"), col("city")
	if zipIdx < 0 || latIdx < 0 || lonIdx < 0 {
		return nil, fmt.Errorf("CSV missing required columns (zip, lat, lng); got %v", header)
	}

	var zips []ZipLocation
	for {
		rec, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("WARN: skipping malformed row: %v", err)
			continue
		}
		if len(rec) <= max3(zipIdx, latIdx, lonIdx) {
			continue
		}
		lat, err1 := strconv.ParseFloat(rec[latIdx], 64)
		lon, err2 := strconv.ParseFloat(rec[lonIdx], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		city := ""
		if cityIdx >= 0 && cityIdx < len(rec) {
			city = rec[cityIdx]
		}
		zips = append(zips, ZipLocation{Zip: rec[zipIdx], Lat: lat, Lon: lon, City: city})
	}
	return zips, nil
}

func max3(a, b, c int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

// spatialDedup returns a subset of zips where every retained point is at least
// `minSpacing` miles from every other retained point. Uses a coarse lat/lon
// bucket grid (size ≈ minSpacing/69 deg) to avoid O(n^2).
func spatialDedup(zips []ZipLocation, minSpacing float64) []ZipLocation {
	if minSpacing <= 0 || len(zips) == 0 {
		return zips
	}
	bucketDeg := minSpacing / 69.0
	type bucketKey struct{ lat, lon int }
	buckets := map[bucketKey][]ZipLocation{}
	out := make([]ZipLocation, 0, len(zips)/4)

	keyFor := func(lat, lon float64) bucketKey {
		return bucketKey{
			lat: int(math.Floor(lat / bucketDeg)),
			lon: int(math.Floor(lon / bucketDeg)),
		}
	}

	for _, z := range zips {
		k := keyFor(z.Lat, z.Lon)
		tooClose := false
	check:
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				nk := bucketKey{k.lat + dx, k.lon + dy}
				for _, n := range buckets[nk] {
					if haversineMiles(z.Lat, z.Lon, n.Lat, n.Lon) < minSpacing {
						tooClose = true
						break check
					}
				}
			}
		}
		if !tooClose {
			buckets[k] = append(buckets[k], z)
			out = append(out, z)
		}
	}
	return out
}

// --- Rate limiter ---

func jitteredSleep(meanMs int) {
	if meanMs <= 0 {
		return
	}
	rngMu.Lock()
	jitter := 0.5 + rng.Float64()
	rngMu.Unlock()
	time.Sleep(time.Duration(float64(meanMs)*jitter) * time.Millisecond)
}

func pickUserAgent() string {
	rngMu.Lock()
	defer rngMu.Unlock()
	return userAgents[rng.Intn(len(userAgents))]
}

// --- API call ---

// fetchPage performs a single API call with retries. Returns (response,
// isPaginationCapHit, error). isPaginationCapHit means the API responded
// with errorCode!=0 + "Exceeded limit" — the data is unpaginatable past
// this point and the caller should subdivide.
func fetchPage(sp SearchPoint, start, rows int) (*BrokerResponse, bool, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, false, err
	}
	q := req.URL.Query()
	q.Set("lat", strconv.FormatFloat(sp.Lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(sp.Lon, 'f', 6, 64))
	q.Set("includePrevious", "true")
	q.Set("hl", "true")
	q.Set("nrows", strconv.Itoa(rows))
	q.Set("start", strconv.Itoa(start))
	q.Set("r", strconv.FormatFloat(sp.Radius, 'f', 0, 64))
	q.Set("sort", "score+desc")
	q.Set("wt", "json")
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", pickUserAgent())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://brokercheck.finra.org/")
	req.Header.Set("Origin", "https://brokercheck.finra.org")

	var lastErr error
	for attempt := 1; attempt <= *flagMaxRetries; attempt++ {
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			log.Printf("  attempt %d: transport error: %v", attempt, err)
			backoffSleep(attempt)
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			retryAfter := resp.Header.Get("Retry-After")
			resp.Body.Close()
			log.Printf("  attempt %d: HTTP %d (Retry-After=%s)", attempt, resp.StatusCode, retryAfter)
			if retryAfter != "" {
				if secs, err := strconv.Atoi(retryAfter); err == nil {
					time.Sleep(time.Duration(secs) * time.Second)
					continue
				}
			}
			backoffSleep(attempt)
			continue
		}

		if resp.StatusCode != 200 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			backoffSleep(attempt)
			continue
		}

		var br BrokerResponse
		if err := json.Unmarshal(body, &br); err != nil {
			return nil, false, fmt.Errorf("decode: %w", err)
		}
		if br.ErrorCode != 0 || strings.Contains(strings.ToLower(br.ErrorMessage), "exceeded") {
			return &br, true, nil
		}
		return &br, false, nil
	}
	return nil, false, fmt.Errorf("exhausted retries: %v", lastErr)
}

func backoffSleep(attempt int) {
	rngMu.Lock()
	jitter := 0.7 + rng.Float64()*0.6
	rngMu.Unlock()
	base := 5.0 * math.Pow(3, float64(attempt-1))
	time.Sleep(time.Duration(base * jitter * float64(time.Second)))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- Scraping a single search point with adaptive subdivision ---

// scrapePoint fetches all brokers within sp's circle. If the area has more
// brokers than the API will paginate through (apiPaginationCap), it
// subdivides into 4 quadrants with smaller radius and recurses up to
// maxSubdivideDepth.
//
// Each broker is sent to the emit callback. Returns total records emitted
// (with duplicates across overlapping subdivisions; final dedup at end).
func scrapePoint(sp SearchPoint, emit func(BrokerSource), delayMs int) (int, error) {
	jitteredSleep(delayMs)

	first, capped, err := fetchPage(sp, 0, pageSize)
	if err != nil {
		return 0, err
	}
	if capped {
		log.Printf("  unexpected cap on first page of %s; treating as 0", sp.ID)
		return 0, nil
	}
	total := first.Hits.Total
	if total == 0 {
		return 0, nil
	}

	emitted := 0
	for _, h := range first.Hits.Hits {
		emit(h.Source)
		emitted++
	}

	if total > apiPaginationCap {
		if sp.Depth >= maxSubdivideDepth {
			log.Printf("  WARN: %s has %d brokers but max subdivide depth reached (radius=%.1fmi); some brokers will be missed", sp.ID, total, sp.Radius)
			extra, _ := paginate(sp, first, emit, delayMs)
			return emitted + extra, nil
		}
		log.Printf("  %s has %d brokers > cap; subdividing (radius %.1f → %.1f, depth %d)", sp.ID, total, sp.Radius, sp.Radius/2, sp.Depth+1)
		// Don't paginate the parent — pagination would only return the top
		// 9000 by score. The 4 subdivisions cover the same area.
		subEmitted := 0
		for _, child := range subdivide(sp) {
			n, err := scrapePoint(child, emit, delayMs)
			subEmitted += n
			if err != nil {
				log.Printf("  subdivision %s error: %v", child.ID, err)
			}
		}
		return emitted + subEmitted, nil
	}

	extra, err := paginate(sp, first, emit, delayMs)
	return emitted + extra, err
}

func paginate(sp SearchPoint, first *BrokerResponse, emit func(BrokerSource), delayMs int) (int, error) {
	total := first.Hits.Total
	if total <= pageSize {
		return 0, nil
	}
	emitted := 0
	for start := pageSize; start < total; start += pageSize {
		rows := pageSize
		if start+rows > apiPaginationCap {
			rows = apiPaginationCap - start
			if rows <= 0 {
				break
			}
		}
		jitteredSleep(delayMs)
		resp, capped, err := fetchPage(sp, start, rows)
		if err != nil {
			return emitted, fmt.Errorf("page start=%d: %w", start, err)
		}
		if capped {
			break
		}
		if len(resp.Hits.Hits) == 0 {
			break
		}
		for _, h := range resp.Hits.Hits {
			emit(h.Source)
			emitted++
		}
		if len(resp.Hits.Hits) < rows {
			break
		}
	}
	return emitted, nil
}

// subdivide returns 4 SearchPoints offset NE/SE/SW/NW from the parent's
// center, each with half the radius. Sub-circles overlap to avoid gaps.
func subdivide(sp SearchPoint) []SearchPoint {
	newR := sp.Radius / 2
	off := newR * 0.7
	bearings := []struct {
		bearing float64
		label   string
	}{
		{45, "NE"}, {135, "SE"}, {225, "SW"}, {315, "NW"},
	}
	out := make([]SearchPoint, 0, 4)
	for _, b := range bearings {
		lat, lon := offsetMiles(sp.Lat, sp.Lon, off, b.bearing)
		out = append(out, SearchPoint{
			ID:     fmt.Sprintf("%s>%s", sp.ID, b.label),
			Lat:    lat,
			Lon:    lon,
			Radius: newR,
			Depth:  sp.Depth + 1,
		})
	}
	return out
}

// --- Persistence: streaming output + checkpoint ---

type streamWriter struct {
	mu       sync.Mutex
	out      *os.File
	progress *os.File
}

func newStreamWriter(outDir string) (*streamWriter, error) {
	out, err := os.OpenFile(outDir+"/brokers.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	prog, err := os.OpenFile(outDir+"/progress.jsonl", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		out.Close()
		return nil, err
	}
	return &streamWriter{out: out, progress: prog}, nil
}

func (s *streamWriter) emit(b BrokerSource) {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc, err := json.Marshal(b)
	if err != nil {
		return
	}
	s.out.Write(enc)
	s.out.Write([]byte("\n"))
}

func (s *streamWriter) markDone(id string, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := map[string]any{
		"id":    id,
		"total": total,
		"ts":    time.Now().UTC().Format(time.RFC3339),
	}
	enc, _ := json.Marshal(rec)
	s.progress.Write(enc)
	s.progress.Write([]byte("\n"))
}

func (s *streamWriter) close() {
	s.out.Close()
	s.progress.Close()
}

func loadCompleted(outDir string) (map[string]bool, error) {
	done := map[string]bool{}
	f, err := os.Open(outDir + "/progress.jsonl")
	if err != nil {
		if os.IsNotExist(err) {
			return done, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var rec struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &rec); err == nil && rec.ID != "" {
			done[rec.ID] = true
		}
	}
	return done, sc.Err()
}

// --- Final dedup pass ---

func dedupeStream(outDir string) error {
	f, err := os.Open(outDir + "/brokers.jsonl")
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 4<<20)
	uniq := map[string]BrokerSource{}
	read := 0
	for sc.Scan() {
		var b BrokerSource
		if err := json.Unmarshal(sc.Bytes(), &b); err != nil {
			continue
		}
		read++
		if b.CRD != "" {
			uniq[b.CRD] = b
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	log.Printf("dedup: read=%d unique=%d", read, len(uniq))

	list := make([]BrokerSource, 0, len(uniq))
	for _, b := range uniq {
		list = append(list, b)
	}
	if err := writeJSON(outDir+"/brokers_unique.json", list); err != nil {
		return err
	}
	return writeCSV(outDir+"/brokers_unique.csv", list)
}

func writeJSON(path string, data []BrokerSource) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

func writeCSV(path string, data []BrokerSource) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	w.Write([]string{"CRD", "FirstName", "LastName", "FirmName", "FirmCity", "FirmState", "FirmZip"})
	for _, b := range data {
		var firm, city, state, zip string
		if len(b.CurrentEmployments) > 0 {
			e := b.CurrentEmployments[0]
			firm, city, state, zip = e.FirmName, e.City, e.State, e.Zip
		}
		w.Write([]string{b.CRD, b.FirstName, b.LastName, firm, city, state, zip})
	}
	return nil
}

// --- Main ---

func main() {
	flag.Parse()

	if *flagDedupeOnly {
		log.Println("dedupe-only mode: reading brokers.jsonl")
		if err := dedupeStream(*flagOutDir); err != nil {
			log.Fatalf("dedupe failed: %v", err)
		}
		log.Println("done")
		return
	}

	log.Printf("loading zip codes from %s", *flagZipFile)
	zips, err := loadZipCodes(*flagZipFile)
	if err != nil {
		log.Fatalf("load zips: %v", err)
	}
	log.Printf("loaded %d zip codes", len(zips))

	if *flagMinSpacing > 0 {
		before := len(zips)
		zips = spatialDedup(zips, *flagMinSpacing)
		log.Printf("spatial dedup: %d → %d points (min spacing %.1f mi)", before, len(zips), *flagMinSpacing)
	}

	completed, err := loadCompleted(*flagOutDir)
	if err != nil {
		log.Fatalf("load progress: %v", err)
	}
	if *flagResume && len(completed) > 0 {
		log.Printf("resume: %d points already completed", len(completed))
	}

	var points []SearchPoint
	for _, z := range zips {
		sp := SearchPoint{ID: z.Zip, Lat: z.Lat, Lon: z.Lon, Radius: *flagInitRadius}
		if *flagResume && completed[sp.ID] {
			continue
		}
		points = append(points, sp)
	}
	if *flagLimit > 0 && len(points) > *flagLimit {
		points = points[:*flagLimit]
	}
	log.Printf("scraping %d search points", len(points))

	sw, err := newStreamWriter(*flagOutDir)
	if err != nil {
		log.Fatalf("open output: %v", err)
	}
	defer sw.close()

	start := time.Now()
	totalEmitted := 0

	if *flagWorkers <= 1 {
		for i, sp := range points {
			n, err := scrapePoint(sp, sw.emit, *flagDelayMs)
			totalEmitted += n
			if err != nil {
				log.Printf("[%d/%d] %s error: %v", i+1, len(points), sp.ID, err)
				continue
			}
			sw.markDone(sp.ID, n)
			if (i+1)%10 == 0 || i+1 == len(points) {
				elapsed := time.Since(start)
				rate := float64(i+1) / elapsed.Seconds()
				eta := time.Duration(float64(len(points)-i-1)/rate) * time.Second
				log.Printf("[%d/%d] %s: +%d brokers | rate=%.2f pts/s | ETA=%s | total=%d",
					i+1, len(points), sp.ID, n, rate, eta.Round(time.Second), totalEmitted)
			}
		}
	} else {
		var wg sync.WaitGroup
		jobs := make(chan SearchPoint, *flagWorkers*2)
		var counter struct {
			mu   sync.Mutex
			done int
			emit int
		}
		for w := 0; w < *flagWorkers; w++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for sp := range jobs {
					n, err := scrapePoint(sp, sw.emit, *flagDelayMs)
					if err != nil {
						log.Printf("worker %d %s error: %v", id, sp.ID, err)
					} else {
						sw.markDone(sp.ID, n)
					}
					counter.mu.Lock()
					counter.done++
					counter.emit += n
					if counter.done%10 == 0 {
						log.Printf("[%d/%d] total=%d", counter.done, len(points), counter.emit)
					}
					counter.mu.Unlock()
				}
			}(w)
		}
		for _, sp := range points {
			jobs <- sp
		}
		close(jobs)
		wg.Wait()
		totalEmitted = counter.emit
	}

	log.Printf("scraping complete: %d records emitted in %s", totalEmitted, time.Since(start).Round(time.Second))
	log.Println("running final dedup pass...")
	if err := dedupeStream(*flagOutDir); err != nil {
		log.Fatalf("dedup: %v", err)
	}
	log.Println("done")
}
