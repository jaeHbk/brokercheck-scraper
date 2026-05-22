package main

// --- Search-API response types ---

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
