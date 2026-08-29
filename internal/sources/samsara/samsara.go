// Package samsara is a minimal client for the Samsara Fleet API.
package samsara

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	baseURL           = "https://api.samsara.com"
	vehiclesPageLimit = 512
	// statsChunkSize mirrors the reference backend: /fleet/vehicles/stats is
	// called in chunks of 10 vehicle IDs, not one request for the whole fleet.
	statsChunkSize = 10
)

type Client struct {
	apiToken   string
	httpClient *http.Client
}

func NewClient(apiToken string) *Client {
	return &Client{apiToken: apiToken, httpClient: &http.Client{}}
}

type Vehicle struct {
	ID   string
	Name string
}

// IsDeactivated matches the convention used elsewhere for this fleet: Samsara
// leaves decommissioned vehicles in the account with a marker in the name
// rather than deleting them.
func IsDeactivated(name string) bool {
	n := strings.ToLower(name)
	return n == "" || strings.Contains(n, "deactivated") || strings.Contains(n, "previously paired")
}

// ListVehicles fetches every vehicle on the account (paginated).
func (c *Client) ListVehicles(ctx context.Context) ([]Vehicle, error) {
	var out []Vehicle
	after := ""

	for {
		var page struct {
			Data []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"data"`
			Pagination struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pagination"`
		}

		q := map[string]string{"limit": strconv.Itoa(vehiclesPageLimit)}
		if after != "" {
			q["after"] = after
		}
		if err := c.get(ctx, "/fleet/vehicles", q, &page); err != nil {
			return nil, err
		}

		for _, v := range page.Data {
			out = append(out, Vehicle{ID: v.ID, Name: v.Name})
		}

		if !page.Pagination.HasNextPage {
			break
		}
		after = page.Pagination.EndCursor
	}

	return out, nil
}

// LiveLevels is one vehicle's live fuel/DEF reading from /fleet/vehicles/stats.
// Either field may be nil if that vehicle has no current reading for it.
type LiveLevels struct {
	FuelPercent *float64
	DEFPercent  *float64
}

// GetVehicleLiveLevels fetches the current live fuel level and DEF
// (diesel exhaust fluid) level, both as percent, for each of the given
// vehicle IDs — one combined request per chunk of statsChunkSize (confirmed
// live that both stat types can be requested together). A vehicle with
// neither reading (no sensor, no recent data) is simply absent from the
// returned map.
func (c *Client) GetVehicleLiveLevels(ctx context.Context, vehicleIDs []string) (map[string]LiveLevels, error) {
	result := make(map[string]LiveLevels, len(vehicleIDs))

	for i := 0; i < len(vehicleIDs); i += statsChunkSize {
		end := i + statsChunkSize
		if end > len(vehicleIDs) {
			end = len(vehicleIDs)
		}
		chunk := vehicleIDs[i:end]

		var resp struct {
			Data []struct {
				ID string `json:"id"`
				// The request param is "fuelPercents" (plural) but the
				// response field is "fuelPercent" (singular) — confirmed
				// against the real API, not assumed; getting this wrong
				// silently leaves the value nil with no error, which is
				// exactly what happened before this was caught (0 out of
				// 306 vehicles ever "had" a reading).
				FuelPercent *struct {
					Value float64 `json:"value"`
				} `json:"fuelPercent"`
				// defLevelMilliPercent: request param and response field
				// match exactly (confirmed live). Value is in milli-percent
				// (e.g. 58800 == 58.8%) — divide by 1000.
				DEFLevelMilliPercent *struct {
					Value float64 `json:"value"`
				} `json:"defLevelMilliPercent"`
			} `json:"data"`
		}

		q := map[string]string{
			"vehicleIds": strings.Join(chunk, ","),
			"types":      "fuelPercents,defLevelMilliPercent",
		}
		if err := c.get(ctx, "/fleet/vehicles/stats", q, &resp); err != nil {
			return nil, err
		}

		for _, v := range resp.Data {
			var lv LiveLevels
			if v.FuelPercent != nil {
				fp := v.FuelPercent.Value
				lv.FuelPercent = &fp
			}
			if v.DEFLevelMilliPercent != nil {
				dp := v.DEFLevelMilliPercent.Value / 1000
				lv.DEFPercent = &dp
			}
			if lv.FuelPercent != nil || lv.DEFPercent != nil {
				result[v.ID] = lv
			}
		}
	}

	return result, nil
}

// FuelEnergyReport is one vehicle's row from the /fleet/reports/vehicles/fuel-energy
// report. EfficiencyMPG is Samsara's "efficiencyMpge" field, which is numerically
// equal to real MPG for fuel (non-electric) vehicles.
type FuelEnergyReport struct {
	VehicleID     string
	EfficiencyMPG *float64
}

// GetFuelEnergyReports fetches the fuel-energy report for the given vehicle
// IDs over [start, end). Callers should pass a settled window (see the
// SettledFuelReportWindow doc comment) — Samsara's own docs warn the most
// recent 72 hours of this report may still be processing.
func (c *Client) GetFuelEnergyReports(ctx context.Context, vehicleIDs []string, start, end time.Time) ([]FuelEnergyReport, error) {
	if len(vehicleIDs) == 0 {
		return nil, nil
	}

	var out []FuelEnergyReport
	after := ""

	for {
		var resp struct {
			Data struct {
				VehicleReports []struct {
					EfficiencyMpge *float64 `json:"efficiencyMpge"`
					Vehicle        struct {
						ID string `json:"id"`
					} `json:"vehicle"`
				} `json:"vehicleReports"`
			} `json:"data"`
			Pagination struct {
				EndCursor   string `json:"endCursor"`
				HasNextPage bool   `json:"hasNextPage"`
			} `json:"pagination"`
		}

		q := map[string]string{
			"startDate":  start.UTC().Format("2006-01-02T15:04:05Z"),
			"endDate":    end.UTC().Format("2006-01-02T15:04:05Z"),
			"vehicleIds": strings.Join(vehicleIDs, ","),
		}
		if after != "" {
			q["after"] = after
		}
		if err := c.get(ctx, "/fleet/reports/vehicles/fuel-energy", q, &resp); err != nil {
			return nil, err
		}

		for _, r := range resp.Data.VehicleReports {
			if r.Vehicle.ID == "" {
				continue
			}
			out = append(out, FuelEnergyReport{VehicleID: r.Vehicle.ID, EfficiencyMPG: r.EfficiencyMpge})
		}

		if !resp.Pagination.HasNextPage {
			break
		}
		after = resp.Pagination.EndCursor
	}

	return out, nil
}

// settleDelayDays mirrors the reference backend's _SETTLE_DELAY_DAYS: the
// most recent 72 hours of the fuel-energy report may still be processing per
// Samsara's docs, so the pulled window always ends 3 days before "now."
const settleDelayDays = 3

// SettledFuelReportWindow returns [start, end) for one full UTC calendar day
// ending settleDelayDays before now — e.g. for "now" = Aug 28, that's
// [Aug 25 00:00 UTC, Aug 26 00:00 UTC). Re-pulling the same window later is
// safe since the report upsert is idempotent.
func SettledFuelReportWindow(now time.Time) (start, end time.Time) {
	nowUTC := now.UTC()
	todayMidnight := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
	end = todayMidnight.AddDate(0, 0, -(settleDelayDays - 1))
	start = end.AddDate(0, 0, -1)
	return start, end
}

func (c *Client) get(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiToken)
	req.Header.Set("Accept", "application/json")

	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("samsara request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("samsara request %s: unexpected status %s", path, resp.Status)
	}

	return json.NewDecoder(resp.Body).Decode(out)
}
