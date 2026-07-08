package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OSVSeverity string

const (
	OSVCritical OSVSeverity = "CRITICAL"
	OSVHigh     OSVSeverity = "HIGH"
	OSVMedium   OSVSeverity = "MEDIUM"
	OSVLow      OSVSeverity = "LOW"
	OSVUnknown  OSVSeverity = "UNKNOWN"
)

type OSVAdvisory struct {
	ID       string
	Summary  string
	Severity OSVSeverity
}

type OSVResult struct {
	Count       int
	MaxSeverity OSVSeverity
	Advisories  []OSVAdvisory
}

var osvQueryURL = "https://api.osv.dev/v1/query"

var severityRank = map[OSVSeverity]int{
	OSVCritical: 4,
	OSVHigh:     3,
	OSVMedium:   2,
	OSVLow:      1,
	OSVUnknown:  0,
}

func MaxOSVSeverity(sevs []OSVSeverity) OSVSeverity {
	if len(sevs) == 0 {
		return OSVUnknown
	}
	max := sevs[0]
	for _, s := range sevs[1:] {
		if severityRank[s] > severityRank[max] {
			max = s
		}
	}
	return max
}

func cvssScoreToSeverity(score string) OSVSeverity {
	var f float64
	if _, err := parseFloat(score, &f); err == nil {
		switch {
		case f >= 9.0:
			return OSVCritical
		case f >= 7.0:
			return OSVHigh
		case f >= 4.0:
			return OSVMedium
		case f >= 0.1:
			return OSVLow
		}
	}
	return OSVUnknown
}

func parseFloat(s string, f *float64) (interface{}, error) {
	// Simple float parse
	var result float64
	n, err := fmt.Sscanf(s, "%f", &result)
	if n == 1 && err == nil {
		*f = result
		return nil, nil
	}
	return nil, fmt.Errorf("not a float")
}

func parseOSVSeverity(vuln map[string]interface{}) OSVSeverity {
	// Check database_specific.severity
	if dbSpec, ok := vuln["database_specific"].(map[string]interface{}); ok {
		if sev, ok := dbSpec["severity"].(string); ok {
			upper := strings.ToUpper(sev)
			switch OSVSeverity(upper) {
			case OSVCritical, OSVHigh, OSVMedium, OSVLow:
				return OSVSeverity(upper)
			}
		}
	}

	// Fall back to CVSS
	if sevArr, ok := vuln["severity"].([]interface{}); ok {
		for _, s := range sevArr {
			if sMap, ok := s.(map[string]interface{}); ok {
				t, _ := sMap["type"].(string)
				score, _ := sMap["score"].(string)
				if (t == "CVSS_V3" || t == "CVSS_V2") && score != "" {
					return cvssScoreToSeverity(score)
				}
			}
		}
	}
	return OSVUnknown
}

func FetchOSVAdvisories(ownerRepo string, timeoutMs int) *OSVResult {
	if timeoutMs <= 0 {
		timeoutMs = 3000
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()

	payload := map[string]interface{}{
		"package": map[string]string{
			"purl": "pkg:github/" + ownerRepo,
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", osvQueryURL, bytes.NewReader(body))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var result struct {
		Vulns []map[string]interface{} `json:"vulns"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil
	}

	var advisories []OSVAdvisory
	for _, v := range result.Vulns {
		id, _ := v["id"].(string)
		summary, _ := v["summary"].(string)
		sev := parseOSVSeverity(v)
		advisories = append(advisories, OSVAdvisory{ID: id, Summary: summary, Severity: sev})
	}

	if len(advisories) == 0 {
		return &OSVResult{Count: 0}
	}

	sevs := make([]OSVSeverity, len(advisories))
	for i, a := range advisories {
		sevs[i] = a.Severity
	}

	return &OSVResult{
		Count:       len(advisories),
		MaxSeverity: MaxOSVSeverity(sevs),
		Advisories:  advisories,
	}
}
