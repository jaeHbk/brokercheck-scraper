package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
)

var errBrokerNotFound = errors.New("broker not found")

// parseDetail unwraps the outer envelope, parses the nested content
// JSON string into a BrokerDetail, and stamps the CRD.
func parseDetail(crd string, body []byte) (*BrokerDetail, error) {
	var env DetailEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("envelope: %w", err)
	}
	if env.Hits.Total == 0 || len(env.Hits.Hits) == 0 {
		return nil, errBrokerNotFound
	}
	contentStr := env.Hits.Hits[0].Source.Content
	if contentStr == "" {
		return nil, fmt.Errorf("empty content field")
	}
	var d BrokerDetail
	if err := json.Unmarshal([]byte(contentStr), &d); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	d.CRD = crd
	return &d, nil
}

func runEnrich() {
	log.Fatal("enrich subcommand not yet implemented (parser only)")
}
