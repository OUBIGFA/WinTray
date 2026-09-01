// Package update checks the upstream GitHub project for a newer release.
// The check only runs when the user explicitly asks for it.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"wintray/internal/version"
)

// Result describes the outcome of a single update check.
type Result struct {
	Current   string
	Latest    string
	PageURL   string
	HasUpdate bool
}

const requestTimeout = 12 * time.Second

// Check asks GitHub for the latest published release and compares it with the
// running build.
func Check(ctx context.Context, current string) (Result, error) {
	return checkAt(ctx, version.LatestReleaseAPI, current)
}

func checkAt(ctx context.Context, apiURL, current string) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "WinTray/"+version.Number)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("github responded with status %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}

	latest := Normalize(payload.TagName)
	if latest == "" {
		return Result{}, fmt.Errorf("github returned no release tag")
	}

	pageURL := strings.TrimSpace(payload.HTMLURL)
	if pageURL == "" {
		pageURL = version.RepositoryURL
	}

	return Result{
		Current:   Normalize(current),
		Latest:    latest,
		PageURL:   pageURL,
		HasUpdate: Compare(latest, current) > 0,
	}, nil
}

// Normalize strips the conventional "v" prefix and surrounding spaces.
func Normalize(tag string) string {
	trimmed := strings.TrimSpace(tag)
	if len(trimmed) > 1 && (trimmed[0] == 'v' || trimmed[0] == 'V') && isDigit(trimmed[1]) {
		return trimmed[1:]
	}
	return trimmed
}

// Compare orders two versions: >0 when a is newer than b, 0 when equal.
// Numeric components rank first; a pre-release suffix (1.2.0-beta) ranks below
// the plain release it belongs to.
func Compare(a, b string) int {
	numsA, preA := split(a)
	numsB, preB := split(b)

	for i := 0; i < len(numsA) || i < len(numsB); i++ {
		if diff := at(numsA, i) - at(numsB, i); diff != 0 {
			return sign(diff)
		}
	}
	if preA == preB {
		return 0
	}
	if preA == "" {
		return 1
	}
	if preB == "" {
		return -1
	}
	return strings.Compare(preA, preB)
}

func split(v string) ([]int, string) {
	normalized := Normalize(v)
	pre := ""
	if idx := strings.IndexAny(normalized, "-+ "); idx >= 0 {
		pre = normalized[idx+1:]
		normalized = normalized[:idx]
	}

	parts := strings.Split(normalized, ".")
	nums := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			// A non-numeric build name (e.g. "dev-20240101") has no ordering
			// information; treat it as the lowest possible version so any
			// published release counts as newer.
			return nil, "dev"
		}
		nums = append(nums, n)
	}
	return nums, pre
}

func at(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

func sign(v int) int {
	if v > 0 {
		return 1
	}
	return -1
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
