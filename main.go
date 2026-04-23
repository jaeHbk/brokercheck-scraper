package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	//"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// --- Structs to Match the JSON Response (Unchanged) ---
type BrokerResponse struct {
	Hits HitData `json:"hits"`
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

// --- New Structs for Scaled-Up Scraping ---

// ZipLocation holds data from our CSV
type ZipLocation struct {
	Zip      string
	Latitude string
	Longitude string
}

// Job represents one zip code to scrape
type Job struct {
	Zip ZipLocation
}

// Result holds the brokers found for one Job
type Result struct {
	Job     Job
	Brokers []BrokerSource
	Err     error
}

// --- API & Scraper Parameters ---
const (
	apiURL       = "https://api.brokercheck.finra.org/search/individual"
	radius       = "25"  // 25-mile radius (this will cause overlap)
	pageSize     = 100 // Max results per page
	numWorkers   = 10  // Number of concurrent scrapers. Be careful not to set too high!
	zipCodeFile  = "uszips.csv"
	maxRetries   = 3   // Retries for failed HTTP requests
)

// Global HTTP client for connection reuse
var client = &http.Client{Timeout: 15 * time.Second}

// --- Worker Function ---
// This function runs the pagination loop for a single zip code
func worker(id int, jobs <-chan Job, results chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("Worker %d started...", id)
	for job := range jobs {
		var pageBrokers []BrokerSource
		currentPage := 0
		totalResults := 0

		log.Printf("Worker %d: Starting job for Zip %s", id, job.Zip.Zip)

		for {
			start := currentPage * pageSize
			if totalResults > 0 && start >= totalResults {
				break // We've got all pages for this zip
			}

			//delay := time.Duration(2+rand.Intn(4)) * time.Second
        	//time.Sleep(delay)

			response, err := fetchBrokerData(job.Zip.Latitude, job.Zip.Longitude, start, pageSize)
			if err != nil {
				log.Printf("Worker %d: ERROR on Zip %s (page %d): %v", id, job.Zip.Zip, currentPage, err)
				results <- Result{Job: job, Err: err} // Send error back
				break // Stop processing this job
			}

			if totalResults == 0 {
				totalResults = response.Hits.Total
				if totalResults == 0 {
					break // No results for this zip
				}
			}

			for _, hit := range response.Hits.Hits {
				pageBrokers = append(pageBrokers, hit.Source)
			}

			if len(response.Hits.Hits) < pageSize {
				break // This was the last page
			}

			currentPage++
			// No sleep needed here, we throttle by numWorkers
		}
		
		log.Printf("Worker %d: Finished job for Zip %s. Found %d brokers.", id, job.Zip.Zip, len(pageBrokers))
		results <- Result{Job: job, Brokers: pageBrokers}
	}
	log.Printf("Worker %d finished.", id)
}

// --- Main Function ---
func main() {
	// --- 1. Load Zip Codes ---
	log.Println("Loading zip codes from", zipCodeFile)
	zips, err := loadZipCodes(zipCodeFile)
	if err != nil {
		log.Fatalf("Failed to load zip codes: %v", err)
	}
	log.Printf("Loaded %d zip codes.", len(zips))

	// --- 2. Setup Worker Pool ---
	jobs := make(chan Job, len(zips))
	results := make(chan Result, len(zips))

	var wg sync.WaitGroup
	for w := 1; w <= numWorkers; w++ {
		wg.Add(1)
		go worker(w, jobs, results, &wg)
	}

	// --- 3. Send Jobs ---
	for _, zip := range zips {
		jobs <- Job{Zip: zip}
	}
	close(jobs) // All jobs are sent

	// --- 4. Collect & Deduplicate Results ---
	// Start a goroutine to close the results channel once all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	log.Println("Waiting for results... This will take a long time.")
	uniqueBrokers := make(map[string]BrokerSource) // Our deduplication map
	totalScraped := 0
	jobsFailed := 0
	jobsSucceeded := 0

	for res := range results {
		if res.Err != nil {
			log.Printf("Job for zip %s failed: %v", res.Job.Zip.Zip, res.Err)
			jobsFailed++
			continue
		}
		
		jobsSucceeded++
		for _, broker := range res.Brokers {
			if _, exists := uniqueBrokers[broker.CRD]; !exists {
				uniqueBrokers[broker.CRD] = broker
			}
			totalScraped++
		}
	}

	log.Println("--- Scraping Complete ---")
	log.Printf("Total zip code jobs: %d", len(zips))
	log.Printf("Jobs Succeeded: %d", jobsSucceeded)
	log.Printf("Jobs Failed: %d", jobsFailed)
	log.Printf("Total brokers scraped (with duplicates): %d", totalScraped)
	log.Printf("Total unique brokers found: %d", len(uniqueBrokers))

	// --- 5. Convert Map to Slice for Saving ---
	finalBrokerList := make([]BrokerSource, 0, len(uniqueBrokers))
	for _, broker := range uniqueBrokers {
		finalBrokerList = append(finalBrokerList, broker)
	}

	// --- 6. Save Files ---
	log.Println("Saving to JSON and CSV...")
	saveToJSON(finalBrokerList, "brokers_unique.json")
	saveToCSV(finalBrokerList, "brokers_unique.csv")
	log.Println("All done.")
}

// --- Helper: Load Zip Codes ---
func loadZipCodes(filename string) ([]ZipLocation, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	// Uncomment this line if your CSV has a header row
	// if _, err := reader.Read(); err != nil { 
	// 	return nil, err
	// }

	records, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var zips []ZipLocation
	for _, rec := range records {
		if len(rec) < 3 {
			continue // Skip malformed rows
		}
		zips = append(zips, ZipLocation{
			Zip:      rec[0],
			Latitude: rec[1],
			Longitude: rec[2],
		})
	}
	return zips, nil
}


// --- Helper: Fetch Data (with Retries) ---
func fetchBrokerData(lat, lon string, start, rows int) (*BrokerResponse, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Set("lat", lat)
	q.Set("lon", lon)
	q.Set("includePrevious", "true")
	q.Set("hl", "true")
	q.Set("nrows", strconv.Itoa(rows))
	q.Set("start", strconv.Itoa(start))
	q.Set("r", radius)
	q.Set("sort", "score+desc")
	q.Set("wt", "json")
	req.URL.RawQuery = q.Encode()

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	var resp *http.Response
	
	for i := 0; i < maxRetries; i++ {
		resp, err = client.Do(req)
		if err != nil {
			log.Printf("Retry %d/%d: Request failed: %v", i+1, maxRetries, err)
			time.Sleep(2 * time.Second)
			continue
		}
		
		// 429 (Too Many Requests) or 5xx (Server Error)
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			log.Printf("Retry %d/%d: Bad status %d for %s", i+1, maxRetries, resp.StatusCode, req.URL.String())
			resp.Body.Close()
			time.Sleep(5 * time.Second) // Wait longer
			continue
		}
		
		break // Success or non-retriable error
	}
	
	if err != nil {
		return nil, fmt.Errorf("request failed after %d retries: %v", maxRetries, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bad status code: %d for URL: %s", resp.StatusCode, req.URL.String())
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var brokerResponse BrokerResponse
	if err := json.Unmarshal(body, &brokerResponse); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %v. Body: %s", err, string(body))
	}

	return &brokerResponse, nil
}


// --- Utility Functions for Saving (Unchanged) ---

func saveToJSON(data []BrokerSource, filename string) {
	file, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("Error marshaling JSON: %v", err)
		return
	}
	err = os.WriteFile(filename, file, 0644)
	if err != nil {
		log.Printf("Error writing JSON file: %v", err)
	}
	log.Printf("Successfully saved to %s", filename)
}

func saveToCSV(data []BrokerSource, filename string) {
	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Error creating CSV file: %v", err)
		return
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	writer.Write([]string{"CRD", "FirstName", "LastName", "FirmName", "FirmCity", "FirmState", "FirmZip"})
	for _, broker := range data {
		var firmName, city, state, zip string
		if len(broker.CurrentEmployments) > 0 {
			firmName = broker.CurrentEmployments[0].FirmName
			city = broker.CurrentEmployments[0].City
			state = broker.CurrentEmployments[0].State
			zip = broker.CurrentEmployments[0].Zip
		}

		row := []string{broker.CRD, broker.FirstName, broker.LastName, firmName, city, state, zip}
		writer.Write(row)
	}
	log.Printf("Successfully saved to %s", filename)
}