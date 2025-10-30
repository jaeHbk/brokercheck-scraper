package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Structs to Match the JSON Response
// These are built to match the JSON output observed from Broker Check search output.

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

// BrokerSource contains the actual broker data
type BrokerSource struct {
	CRD                string       `json:"ind_source_id"`
	FirstName          string       `json:"ind_firstname"`
	LastName           string       `json:"ind_lastname"`
	CurrentEmployments []Employment `json:"ind_current_employments"`
}

// Employment contains the firm's details
type Employment struct {
	FirmName string `json:"firm_name"`
	City     string `json:"branch_city"`
	State    string `json:"branch_state"`
	Zip      string `json:"branch_zip"`
}

// Global HTTP client for connection reuse
var client = &http.Client{Timeout: 10 * time.Second}

// API Search Parameters
// These are from the URL found when inspecting Fetch/XHR of API from Broker Check website
const (
	apiURL   = "https://api.brokercheck.finra.org/search/individual"
	latitude = "38.895568" // For Washington D.C. area (example)
	longitude = "-77.026278" // For Washington D.C. area (example)
	radius   = "25"         // 25-mile radius
	pageSize = 100        // Get 100 results per page (max allowed is often 100 or 50)
)

func main() {
	var allBrokers []BrokerSource
	currentPage := 0
	totalResults := 0 // We'll get this from the first request

	log.Println("Starting scrape...")

	for {
		// Calculate the 'start' parameter for pagination
		start := currentPage * pageSize

		// Break the loop if we've already gathered all results
		if totalResults > 0 && start >= totalResults {
			break
		}

		log.Printf("Fetching page %d (starting at record %d)...", currentPage+1, start)

		response, err := fetchBrokerData(latitude, longitude, start, pageSize)
		if err != nil {
			log.Printf("Error fetching page %d: %v", currentPage+1, err)
			break // Stop on error
		}

		// Set totalResults on the first loop
		if totalResults == 0 {
			totalResults = response.Hits.Total
			if totalResults == 0 {
				log.Println("API returned 0 total results. Exiting.")
				break
			}
			log.Printf("Found %d total results. Starting download...", totalResults)
		}

		// Add the brokers from this page to our main list
		for _, hit := range response.Hits.Hits {
			allBrokers = append(allBrokers, hit.Source)
		}

		// If this was the last page, stop
		if len(response.Hits.Hits) < pageSize {
			break
		}

		currentPage++
		time.Sleep(1 * time.Second) // Be polite! Let's not break the website
	}

	log.Printf("Finished scraping. Found %d brokers.", len(allBrokers))

	// Save the results
	saveToJSON(allBrokers, "brokers.json")
	saveToCSV(allBrokers, "brokers.csv")
}

// fetchBrokerData performs the GET request to the API
func fetchBrokerData(lat, lon string, start, rows int) (*BrokerResponse, error) {
	// Create a new GET request
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	// Build the Query Parameters
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

	// Set Headers
	// Mimic the browser headers. User-Agent is often the most important.
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "application/json")

	// Perform the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("bad status code: %d for URL: %s", resp.StatusCode, req.URL.String())
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Unmarshal the JSON into our structs
	var brokerResponse BrokerResponse
	if err := json.Unmarshal(body, &brokerResponse); err != nil {
		return nil, fmt.Errorf("error unmarshaling JSON: %v. Body: %s", err, string(body))
	}

	return &brokerResponse, nil
}

// Utility Functions for Saving

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

	// Write Header
	// We flatten the data: get the first current employment for the CSV
	writer.Write([]string{"CRD", "FirstName", "LastName", "FirmName", "FirmCity", "FirmState", "FirmZip"})

	// Write Data Rows
	for _, broker := range data {
		var firmName, city, state, zip string

		// Safely get the first employment record
		if len(broker.CurrentEmployments) > 0 {
			firmName = broker.CurrentEmployments[0].FirmName
			city = broker.CurrentEmployments[0].City
			state = broker.CurrentEmployments[0].State
			zip = broker.CurrentEmployments[0].Zip
		}

		row := []string{
			broker.CRD,
			broker.FirstName,
			broker.LastName,
			firmName,
			city,
			state,
			zip,
		}
		writer.Write(row)
	}
	log.Printf("Successfully saved to %s", filename)
}