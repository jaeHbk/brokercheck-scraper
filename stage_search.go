package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
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

// --- API call ---

// fetchPage performs one search-API call. Caller owns the Client; rate
// limiting and retries are handled there.
func fetchPage(ctx context.Context, c *Client, sp SearchPoint, start, rows int) (*BrokerResponse, bool, error) {
	q := url.Values{}
	q.Set("lat", strconv.FormatFloat(sp.Lat, 'f', 6, 64))
	q.Set("lon", strconv.FormatFloat(sp.Lon, 'f', 6, 64))
	q.Set("includePrevious", "true")
	q.Set("hl", "true")
	q.Set("nrows", strconv.Itoa(rows))
	q.Set("start", strconv.Itoa(start))
	q.Set("r", strconv.FormatFloat(sp.Radius, 'f', 0, 64))
	q.Set("sort", "score+desc")
	q.Set("wt", "json")

	full := apiURL + "?" + q.Encode()
	body, err := c.Get(ctx, full)
	if err != nil {
		return nil, false, err
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
func scrapePoint(ctx context.Context, c *Client, sp SearchPoint, emit func(BrokerSource)) (int, error) {
	first, capped, err := fetchPage(ctx, c, sp, 0, pageSize)
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
			extra, _ := paginate(ctx, c, sp, first, emit)
			return emitted + extra, nil
		}
		log.Printf("  %s has %d brokers > cap; subdividing (radius %.1f → %.1f, depth %d)", sp.ID, total, sp.Radius, sp.Radius/2, sp.Depth+1)
		// Don't paginate the parent — pagination would only return the top
		// 9000 by score. The 4 subdivisions cover the same area.
		subEmitted := 0
		for _, child := range subdivide(sp) {
			n, err := scrapePoint(ctx, c, child, emit)
			subEmitted += n
			if err != nil {
				log.Printf("  subdivision %s error: %v", child.ID, err)
			}
		}
		return emitted + subEmitted, nil
	}

	extra, err := paginate(ctx, c, sp, first, emit)
	return emitted + extra, err
}

func paginate(ctx context.Context, c *Client, sp SearchPoint, first *BrokerResponse, emit func(BrokerSource)) (int, error) {
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
		resp, capped, err := fetchPage(ctx, c, sp, start, rows)
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

// --- Search-stage entry points ---

func runSearch() {
	ctx := context.Background()
	rps := *flagRPS
	if rps <= 0 {
		// Back-compat: derive from --delay
		rps = 1000.0 / float64(*flagDelayMs)
	}
	limiter := newLimiter(rps, 0.05)
	client := newClient(limiter, *flagMaxRetries)

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
	log.Printf("scraping %d search points (rps=%.2f workers=%d)", len(points), rps, *flagWorkers)

	sw, err := newStreamWriter(*flagOutDir)
	if err != nil {
		log.Fatalf("open output: %v", err)
	}
	defer sw.close()

	start := time.Now()
	totalEmitted := runSearchWorkers(ctx, client, points, sw)
	log.Printf("scraping complete: %d records emitted in %s", totalEmitted, time.Since(start).Round(time.Second))
	log.Println("running final dedup pass...")
	if err := dedupeStream(*flagOutDir); err != nil {
		log.Fatalf("dedup: %v", err)
	}
	log.Println("done")
}

func runSearchWorkers(ctx context.Context, c *Client, points []SearchPoint, sw *streamWriter) int {
	if *flagWorkers <= 1 {
		total := 0
		startTs := time.Now()
		for i, sp := range points {
			n, err := scrapePoint(ctx, c, sp, sw.emit)
			total += n
			if err != nil {
				log.Printf("[%d/%d] %s error: %v", i+1, len(points), sp.ID, err)
				continue
			}
			sw.markDone(sp.ID, n)
			if (i+1)%10 == 0 || i+1 == len(points) {
				elapsed := time.Since(startTs)
				rate := float64(i+1) / elapsed.Seconds()
				eta := time.Duration(float64(len(points)-i-1)/rate) * time.Second
				log.Printf("[%d/%d] %s: +%d brokers | rate=%.2f pts/s | ETA=%s | total=%d",
					i+1, len(points), sp.ID, n, rate, eta.Round(time.Second), total)
			}
		}
		return total
	}

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
				n, err := scrapePoint(ctx, c, sp, sw.emit)
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
	return counter.emit
}
