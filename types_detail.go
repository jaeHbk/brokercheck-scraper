package main

// --- Detail-API response types ---
//
// The detail endpoint returns a doubly-wrapped JSON document. The outer
// envelope (DetailEnvelope) holds hits.hits[]._source.content, which is
// itself a JSON-encoded string whose unmarshaled shape is BrokerDetail.
// Note: this envelope is NOT the same as HitData in types.go; the _source
// shapes differ.

type DetailEnvelope struct {
	Hits struct {
		Total int `json:"total"`
		Hits  []struct {
			Source struct {
				Content string `json:"content"`
			} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}

type BrokerDetail struct {
	CRD                   string                 `json:"crd"` // injected by the scraper, not from the API
	BasicInfo             BasicInformation       `json:"basicInformation"`
	CurrentEmployments    []DetailedEmployment   `json:"currentEmployments"`
	CurrentIAEmployments  []DetailedEmployment   `json:"currentIAEmployments"`
	PreviousEmployments   []DetailedEmployment   `json:"previousEmployments"`
	PreviousIAEmployments []DetailedEmployment   `json:"previousIAEmployments"`
	Disclosures           []Disclosure           `json:"disclosures"`
	DisclosureFlag        string                 `json:"disclosureFlag"`
	IADisclosureFlag      string                 `json:"iaDisclosureFlag"`
	StateExams            []Exam                 `json:"stateExamCategory"`
	PrincipalExams        []Exam                 `json:"principalExamCategory"`
	ProductExams          []Exam                 `json:"productExamCategory"`
	ExamsCount            ExamsCount             `json:"examsCount"`
	RegisteredStates      []RegisteredState      `json:"registeredStates"`
	RegisteredSROs        []RegisteredSRO        `json:"registeredSROs"`
	RegistrationCount     RegistrationCount      `json:"registrationCount"`
	BrokerDetails         BrokerDetailsInner     `json:"brokerDetails"`
	FetchedAt             string                 `json:"fetched_at"` // injected by the scraper, not from the API
}

type BasicInformation struct {
	IndividualID                 int      `json:"individualId"`
	FirstName                    string   `json:"firstName"`
	MiddleName                   string   `json:"middleName,omitempty"`
	LastName                     string   `json:"lastName"`
	OtherNames                   []string `json:"otherNames,omitempty"`
	BCScope                      string   `json:"bcScope"`
	IAScope                      string   `json:"iaScope"`
	DaysInIndustryCalculatedDate string   `json:"daysInIndustryCalculatedDate,omitempty"`
	DaysInIndustry               int      `json:"daysInIndustry,omitempty"`
	Sanctions                    any      `json:"sanctions,omitempty"`
}

type DetailedEmployment struct {
	FirmID                int               `json:"firmId"`
	FirmName              string            `json:"firmName"`
	IAOnly                string            `json:"iaOnly"`
	RegistrationBeginDate string            `json:"registrationBeginDate"`
	RegistrationEndDate   string            `json:"registrationEndDate,omitempty"`
	FirmBCScope           string            `json:"firmBCScope"`
	FirmIAScope           string            `json:"firmIAScope"`
	IASECNumber           string            `json:"iaSECNumber,omitempty"`
	IASECNumberType       string            `json:"iaSECNumberType,omitempty"`
	BDSECNumber           string            `json:"bdSECNumber,omitempty"`
	BranchOfficeLocations []BranchOfficeLoc `json:"branchOfficeLocations,omitempty"`
	City                  string            `json:"city,omitempty"`
	State                 string            `json:"state,omitempty"`
	Country               string            `json:"country,omitempty"`
}

type BranchOfficeLoc struct {
	DisplayOrder            int    `json:"displayOrder"`
	LocatedAtFlag           string `json:"locatedAtFlag"`
	SupervisedFromFlag      string `json:"supervisedFromFlag"`
	PrivateResidenceFlag    string `json:"privateResidenceFlag"`
	BranchOfficeID          string `json:"branchOfficeId"`
	Street1                 string `json:"street1"`
	Street2                 string `json:"street2,omitempty"`
	City                    string `json:"city"`
	State                   string `json:"state"`
	Country                 string `json:"country"`
	ZipCode                 string `json:"zipCode"`
	Latitude                string `json:"latitude"`
	Longitude               string `json:"longitude"`
	GeoLocation             string `json:"geoLocation"`
	NonRegisteredOfficeFlag string `json:"nonRegisteredOfficeFlag"`
	ELABeginDate            string `json:"elaBeginDate,omitempty"`
}

type Disclosure struct {
	EventDate            string         `json:"eventDate"`
	DisclosureType       string         `json:"disclosureType"`
	DisclosureResolution string         `json:"disclosureResolution"`
	IsIapdExcludedCCFlag string         `json:"isIapdExcludedCCFlag"`
	IsBcExcludedCCFlag   string         `json:"isBcExcludedCCFlag"`
	BCCtgryType          int            `json:"bcCtgryType"`
	DisclosureDetail     map[string]any `json:"disclosureDetail"`
}

type Exam struct {
	ExamCategory  string `json:"examCategory"`
	ExamName      string `json:"examName"`
	ExamTakenDate string `json:"examTakenDate"`
	ExamScope     string `json:"examScope"`
}

type ExamsCount struct {
	StateExamCount     int `json:"stateExamCount"`
	PrincipalExamCount int `json:"principalExamCount"`
	ProductExamCount   int `json:"productExamCount"`
}

type RegisteredState struct {
	State    string `json:"state"`
	RegScope string `json:"regScope"`
	Status   string `json:"status"`
	RegDate  string `json:"regDate"`
}

type RegisteredSRO struct {
	SRO            string   `json:"sro"`
	Status         string   `json:"status"`
	CategoriesList []string `json:"CategoriesList"`
}

type RegistrationCount struct {
	ApprovedSRORegistrationCount     int `json:"approvedSRORegistrationCount"`
	ApprovedFinraRegistrationCount   int `json:"approvedFinraRegistrationCount"`
	ApprovedStateRegistrationCount   int `json:"approvedStateRegistrationCount"`
	ApprovedIAStateRegistrationCount int `json:"approvedIAStateRegistrationCount"`
}

type BrokerDetailsInner struct {
	HasBCComments                 string `json:"hasBCComments"`
	HasIAComments                 string `json:"hasIAComments"`
	LegacyReportStatusDescription string `json:"legacyReportStatusDescription"`
}
