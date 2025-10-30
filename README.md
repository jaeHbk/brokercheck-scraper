# FINRA BrokerCheck API Scraper

This Go script scrapes broker information from FINRA's internal BrokerCheck API.
It's designed to fetch all broker results for a specific geographic coordinate 
(latitude/longitude) by automatically handling the API's pagination. 
The results are then saved to both brokers.json and brokers.csv.

## How it works
This script reverse-engineers the internal API that the BrokerCheck website's front-end uses to fetch data.
- API Endpoint: It sends GET requests directly to the `https://api.brokercheck.finra.org/search/individual` endpoint.
- Search Method: The API searches based on latitude and longitude (lat, lon) within a given radius (r), not by zip code.
- Pagination: The script makes an initial request to find the total number of results. It then calculates how many pages are
  needed (based on the pageSize) and loops, making a new request for each page until all results are downloaded.
- Output: All results are collected into memory and then written to brokers.json (a full JSON array) and brokers.csv (a flattened list for easy viewing).

## How to run
### Prerequisites
You must have Go installed on your system.

### Running the Script
- Open your terminal and navigate to the directory containing the file.
- Run the script: `go run main.go`
- The script will log its progress to the terminal and create the output files in the same directory.


## Resource
[Main website](https://brokercheck.finra.org/)
