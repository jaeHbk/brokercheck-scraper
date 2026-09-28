package main

// PDF-derived data is deliberately independent of website enrichment models.
type ReportRef struct {
	CRD       string `json:"crd"`
	SHA256    string `json:"sha256"`
	SourceURL string `json:"source_url"`
	PDFPath   string `json:"pdf_path"`
	FetchedAt string `json:"fetched_at"`
	Bytes     int64  `json:"bytes"`
	PageCount int    `json:"page_count"`
}
type PDFInfo struct {
	PageCount      int               `json:"page_count"`
	Backend        string            `json:"backend"`
	BackendVersion string            `json:"backend_version"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}
type HTTPMetadata struct {
	StatusCode   int    `json:"status_code"`
	ContentType  string `json:"content_type"`
	ETag         string `json:"etag"`
	LastModified string `json:"last_modified"`
	RetrievedAt  string `json:"retrieved_at"`
}
type Document struct {
	Report           ReportRef    `json:"report"`
	ExtractorVersion string       `json:"extractor_version"`
	Pages            []Page       `json:"pages"`
	Issues           []ParseIssue `json:"issues"`
}
type Page struct {
	Number       int     `json:"number"`
	PrintedLabel string  `json:"printed_label"`
	Width        float64 `json:"width"`
	Height       float64 `json:"height"`
	Text         string  `json:"text"`
	Words        []Word  `json:"words"`
}
type Word struct {
	ID     string  `json:"id"`
	LineID string  `json:"line_id"`
	Text   string  `json:"text"`
	X0     float64 `json:"x0"`
	Y0     float64 `json:"y0"`
	X1     float64 `json:"x1"`
	Y1     float64 `json:"y1"`
}
type Evidence struct {
	Page    int      `json:"page"`
	WordIDs []string `json:"word_ids"`
	RawText string   `json:"raw_text"`
}
type RawDate struct {
	Raw       string  `json:"raw"`
	ISO       *string `json:"iso"`
	Precision string  `json:"precision"`
}
type Field struct {
	ID         string     `json:"id"`
	Path       []string   `json:"path"`
	Label      string     `json:"label"`
	Value      string     `json:"value"`
	Occurrence int        `json:"occurrence"`
	Evidence   []Evidence `json:"evidence"`
}
type SectionState string

const (
	SectionPresent         SectionState = "present"
	SectionExplicitlyEmpty SectionState = "explicitly_empty"
	SectionAbsent          SectionState = "absent"
	SectionUnreadable      SectionState = "unreadable"
	SectionNotApplicable   SectionState = "not_applicable"
)

type ParseIssue struct {
	Code     string     `json:"code"`
	Severity string     `json:"severity"`
	Section  string     `json:"section"`
	Message  string     `json:"message"`
	Evidence []Evidence `json:"evidence"`
}
type Coverage struct {
	Section         string        `json:"section"`
	State           SectionState  `json:"state"`
	RegionWordIDs   []string      `json:"region_word_ids"`
	ConsumedWordIDs []string      `json:"consumed_word_ids"`
	Ignored         []IgnoredSpan `json:"ignored"`
}
type IgnoredSpan struct {
	WordIDs []string `json:"word_ids"`
	Reason  string   `json:"reason"`
}
type Registration struct {
	ID       string     `json:"id"`
	Kind     string     `json:"kind"`
	Scope    string     `json:"scope"`
	FirmName string     `json:"firm_name"`
	FirmCRD  string     `json:"firm_crd"`
	Location string     `json:"location"`
	Start    RawDate    `json:"start"`
	End      RawDate    `json:"end"`
	Fields   []Field    `json:"fields"`
	Evidence []Evidence `json:"evidence"`
}
type EmploymentRecord struct {
	ID                string     `json:"id"`
	Employer          string     `json:"employer"`
	Position          string     `json:"position"`
	InvestmentRelated *bool      `json:"investment_related"`
	Location          string     `json:"location"`
	Start             RawDate    `json:"start"`
	End               RawDate    `json:"end"`
	Fields            []Field    `json:"fields"`
	Evidence          []Evidence `json:"evidence"`
}
type HistoryResult struct {
	Registrations     []Registration     `json:"registrations"`
	Employments       []EmploymentRecord `json:"employments"`
	OtherBusinessText string             `json:"other_business_text"`
	Coverage          []Coverage         `json:"coverage"`
	Issues            []ParseIssue       `json:"issues"`
}
type DisclosureEvent struct {
	ID         string             `json:"id"`
	ReportedID string             `json:"reported_id"`
	Type       string             `json:"type"`
	Status     string             `json:"status"`
	Fields     []Field            `json:"fields"`
	Sources    []DisclosureSource `json:"sources"`
	Evidence   []Evidence         `json:"evidence"`
}
type DisclosureSource struct {
	ID       string     `json:"id"`
	Label    string     `json:"label"`
	Fields   []Field    `json:"fields"`
	Evidence []Evidence `json:"evidence"`
}
type DisclosureCount struct {
	Category string     `json:"category"`
	Status   string     `json:"status"`
	Count    *int       `json:"count"`
	Raw      string     `json:"raw"`
	Evidence []Evidence `json:"evidence"`
}
type DisclosureResult struct {
	Events   []DisclosureEvent `json:"events"`
	Counts   []DisclosureCount `json:"counts"`
	Coverage []Coverage        `json:"coverage"`
	Issues   []ParseIssue      `json:"issues"`
}
type ParsedReport struct {
	Report           ReportRef        `json:"report"`
	SchemaVersion    string           `json:"schema_version"`
	ParserVersion    string           `json:"parser_version"`
	ExtractorVersion string           `json:"extractor_version"`
	History          HistoryResult    `json:"history"`
	Disclosures      DisclosureResult `json:"disclosures"`
	Issues           []ParseIssue     `json:"issues"`
}
type ValidationResult struct {
	Accepted             bool         `json:"accepted"`
	Issues               []ParseIssue `json:"issues"`
	ReportedEventCount   *int         `json:"reported_event_count"`
	ExtractedEventCount  int          `json:"extracted_event_count"`
	RequiredWordCoverage float64      `json:"required_word_coverage"`
}
type DownloadConfig struct {
	OutDir  string
	BaseURL string
}
type ReportsConfig struct {
	OutDir      string
	Input       string
	Workers     int
	RPS         float64
	Retries     int
	Limit       int
	Resume      bool
	Reparse     bool
	Refresh     bool
	RetryFailed bool
	BaseURL     string
}
