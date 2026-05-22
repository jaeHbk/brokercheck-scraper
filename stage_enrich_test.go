package main

import (
	"errors"
	"os"
	"testing"
)

func TestParseDetail_FullPayload(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_2819404.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("2819404", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if d.CRD != "2819404" {
		t.Errorf("CRD: got %q, want 2819404", d.CRD)
	}
	if d.BasicInfo.FirstName != "LAURENCE" {
		t.Errorf("FirstName: got %q", d.BasicInfo.FirstName)
	}
	if d.BasicInfo.MiddleName != "ANTHONY" {
		t.Errorf("MiddleName: got %q", d.BasicInfo.MiddleName)
	}
	if len(d.CurrentEmployments) == 0 {
		t.Errorf("expected current employments")
	}
	if len(d.PreviousEmployments) == 0 {
		t.Errorf("expected previous employments")
	}
	if d.ExamsCount.StateExamCount != 2 {
		t.Errorf("StateExamCount: got %d, want 2", d.ExamsCount.StateExamCount)
	}
	if len(d.RegisteredStates) != 2 {
		t.Errorf("RegisteredStates len: got %d, want 2", len(d.RegisteredStates))
	}
	if len(d.RegisteredSROs) == 0 {
		t.Errorf("expected SROs")
	}
}

func TestParseDetail_WithDisclosures(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_5634972.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	d, err := parseDetail("5634972", body)
	if err != nil {
		t.Fatalf("parseDetail: %v", err)
	}
	if len(d.Disclosures) == 0 {
		t.Fatalf("expected disclosures, got 0")
	}
	if d.Disclosures[0].DisclosureType == "" {
		t.Errorf("disclosure type empty")
	}
	if d.Disclosures[0].DisclosureDetail == nil {
		t.Errorf("disclosure detail empty")
	}
}

func TestParseDetail_NotFound(t *testing.T) {
	body, err := os.ReadFile("testdata/detail_notfound.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	_, err = parseDetail("9999999", body)
	if !errors.Is(err, errBrokerNotFound) {
		t.Fatalf("expected errBrokerNotFound, got %v", err)
	}
}
