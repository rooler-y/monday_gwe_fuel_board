// Package monday is a minimal client for the monday.com GraphQL API (v2),
// scoped to what the fuel board publisher needs: listing a board's items and
// batching item creates/updates into one HTTP request via aliased mutations.
package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const apiURL = "https://api.monday.com/v2"

// requestTimeout bounds every individual HTTP call. Without this, a stalled
// request hangs forever — which is exactly what made an earlier 81-operation
// batch fail as an opaque "Request timed out" with no indication of whether
// monday's server had actually finished the work (it had).
const requestTimeout = 30 * time.Second

type Client struct {
	apiToken   string
	httpClient *http.Client
}

func NewClient(apiToken string) *Client {
	return &Client{apiToken: apiToken, httpClient: &http.Client{Timeout: requestTimeout}}
}

type Item struct {
	ID   string
	Name string
}

// ListItems fetches every item on a board (id, name only), paginated.
func (c *Client) ListItems(ctx context.Context, boardID string) ([]Item, error) {
	const query = `
		query ($boardId: ID!, $cursor: String) {
			boards(ids: [$boardId]) {
				items_page(limit: 100, cursor: $cursor) {
					cursor
					items { id name }
				}
			}
		}
	`

	var out []Item
	var cursor string

	for {
		vars := map[string]any{"boardId": boardID}
		if cursor != "" {
			vars["cursor"] = cursor
		}

		var resp struct {
			Boards []struct {
				ItemsPage struct {
					Cursor string `json:"cursor"`
					Items  []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"items"`
				} `json:"items_page"`
			} `json:"boards"`
		}
		if err := c.execute(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		if len(resp.Boards) == 0 {
			break
		}

		page := resp.Boards[0].ItemsPage
		for _, it := range page.Items {
			out = append(out, Item{ID: it.ID, Name: it.Name})
		}
		if page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}

	return out, nil
}

// itemsChunkSize bounds how many item ids go in one items(ids: [...]) query.
const itemsChunkSize = 100

// GetItemStates returns the current state ("active", "archived", or
// "deleted") for each of the given item ids, chunked. An id absent from the
// returned map means monday has no record of it at all (fully purged).
// Unlike items_page (which only ever returns active items, even when the
// board is queried with state: all — confirmed live), this root-level
// items() query returns an item's real state regardless of whether it's
// still active, which is what makes it useful for detecting stale
// monday_item_id references after an update fails.
func (c *Client) GetItemStates(ctx context.Context, itemIDs []string) (map[string]string, error) {
	const query = `
		query ($ids: [ID!]) {
			items(ids: $ids) { id state }
		}
	`

	out := make(map[string]string, len(itemIDs))
	for i := 0; i < len(itemIDs); i += itemsChunkSize {
		end := i + itemsChunkSize
		if end > len(itemIDs) {
			end = len(itemIDs)
		}
		chunk := itemIDs[i:end]

		var resp struct {
			Items []struct {
				ID    string `json:"id"`
				State string `json:"state"`
			} `json:"items"`
		}
		if err := c.execute(ctx, query, map[string]any{"ids": chunk}, &resp); err != nil {
			return nil, err
		}
		for _, it := range resp.Items {
			out[it.ID] = it.State
		}
	}

	return out, nil
}

// GetTextColumns reads the given text-type columns for every item on a
// board, paginated. Returns item id -> (column id -> text). A column with
// no value for an item is simply absent from that item's inner map. This is
// a read-back of our OWN write target — used narrowly (e.g. checking
// whether a dispatcher has entered a fuel stop before deciding whether to
// compute a Map Link), not as a general data source.
func (c *Client) GetTextColumns(ctx context.Context, boardID string, columnIDs []string) (map[string]map[string]string, error) {
	const query = `
		query ($boardId: ID!, $columnIds: [String!], $cursor: String) {
			boards(ids: [$boardId]) {
				items_page(limit: 100, cursor: $cursor) {
					cursor
					items {
						id
						column_values(ids: $columnIds) { id text }
					}
				}
			}
		}
	`

	out := map[string]map[string]string{}
	var cursor string

	for {
		vars := map[string]any{"boardId": boardID, "columnIds": columnIDs}
		if cursor != "" {
			vars["cursor"] = cursor
		}

		var resp struct {
			Boards []struct {
				ItemsPage struct {
					Cursor string `json:"cursor"`
					Items  []struct {
						ID           string `json:"id"`
						ColumnValues []struct {
							ID   string `json:"id"`
							Text string `json:"text"`
						} `json:"column_values"`
					} `json:"items"`
				} `json:"items_page"`
			} `json:"boards"`
		}
		if err := c.execute(ctx, query, vars, &resp); err != nil {
			return nil, err
		}
		if len(resp.Boards) == 0 {
			break
		}

		page := resp.Boards[0].ItemsPage
		for _, it := range page.Items {
			cols := map[string]string{}
			for _, cv := range it.ColumnValues {
				if cv.Text != "" {
					cols[cv.ID] = cv.Text
				}
			}
			if len(cols) > 0 {
				out[it.ID] = cols
			}
		}
		if page.Cursor == "" {
			break
		}
		cursor = page.Cursor
	}

	return out, nil
}

// ColumnValues is a column_id -> value map, matching monday's per-column-type
// value shapes (plain string for text/numbers, {"item_ids": [...]} for
// board_relation, {"label": ...} for status, {"url": ..., "text": ...} for
// link — confirmed live, not assumed).
type ColumnValues map[string]any

type CreateOp struct {
	BoardID      string
	GroupID      string
	ItemName     string
	ColumnValues ColumnValues
}

type UpdateOp struct {
	BoardID      string
	ItemID       string
	ColumnValues ColumnValues
}

// maxOpsPerRequest bounds how many operations go in one GraphQL request. A
// single 81-operation request to monday's API returned a server-side timeout
// even though the work completed — chunking keeps each request's cost
// predictable and shrinks the blast radius of any one request failing.
const maxOpsPerRequest = 15

// BatchApply issues every create and update as aliased mutations, chunked
// into requests of at most maxOpsPerRequest operations each (monday has no
// bulk mutation that takes an array of items, so each chunk is its own
// GraphQL mutation with one aliased field per operation). Creates and
// updates are chunked independently. Returns the new item ID for each
// create, in the same order as creates — including for creates in a chunk
// that came before a later chunk's failure, since every chunk still gets
// attempted regardless of an earlier one's error.
func (c *Client) BatchApply(ctx context.Context, creates []CreateOp, updates []UpdateOp) ([]string, error) {
	createdIDs := make([]string, len(creates))
	var errs []error

	for start := 0; start < len(creates); start += maxOpsPerRequest {
		end := start + maxOpsPerRequest
		if end > len(creates) {
			end = len(creates)
		}
		ids, err := c.batchOnce(ctx, creates[start:end], nil)
		copy(createdIDs[start:end], ids)
		if err != nil {
			errs = append(errs, fmt.Errorf("creates[%d:%d]: %w", start, end, err))
		}
	}

	for start := 0; start < len(updates); start += maxOpsPerRequest {
		end := start + maxOpsPerRequest
		if end > len(updates) {
			end = len(updates)
		}
		if _, err := c.batchOnce(ctx, nil, updates[start:end]); err != nil {
			errs = append(errs, fmt.Errorf("updates[%d:%d]: %w", start, end, err))
		}
	}

	return createdIDs, errors.Join(errs...)
}

// batchOnce issues one chunk (creates and/or updates) as a single HTTP
// request/GraphQL mutation.
func (c *Client) batchOnce(ctx context.Context, creates []CreateOp, updates []UpdateOp) ([]string, error) {
	if len(creates) == 0 && len(updates) == 0 {
		return nil, nil
	}

	var argDecls []string
	var fields []string
	variables := map[string]any{}

	for i, op := range creates {
		cv, err := json.Marshal(op.ColumnValues)
		if err != nil {
			return nil, fmt.Errorf("marshal column values for create %d: %w", i, err)
		}
		bid, gid, name, cvVar := fmt.Sprintf("cbid%d", i), fmt.Sprintf("cgid%d", i), fmt.Sprintf("cname%d", i), fmt.Sprintf("ccv%d", i)
		argDecls = append(argDecls, fmt.Sprintf("$%s: ID!, $%s: String!, $%s: String!, $%s: JSON!", bid, gid, name, cvVar))
		fields = append(fields, fmt.Sprintf(
			"c%d: create_item(board_id: $%s, group_id: $%s, item_name: $%s, column_values: $%s) { id }",
			i, bid, gid, name, cvVar,
		))
		variables[bid] = op.BoardID
		variables[gid] = op.GroupID
		variables[name] = op.ItemName
		variables[cvVar] = string(cv)
	}

	for i, op := range updates {
		cv, err := json.Marshal(op.ColumnValues)
		if err != nil {
			return nil, fmt.Errorf("marshal column values for update %d: %w", i, err)
		}
		bid, iid, cvVar := fmt.Sprintf("ubid%d", i), fmt.Sprintf("uiid%d", i), fmt.Sprintf("ucv%d", i)
		argDecls = append(argDecls, fmt.Sprintf("$%s: ID!, $%s: ID!, $%s: JSON!", bid, iid, cvVar))
		fields = append(fields, fmt.Sprintf(
			"u%d: change_multiple_column_values(board_id: $%s, item_id: $%s, column_values: $%s) { id }",
			i, bid, iid, cvVar,
		))
		variables[bid] = op.BoardID
		variables[iid] = op.ItemID
		variables[cvVar] = string(cv)
	}

	query := "mutation(" + strings.Join(argDecls, ", ") + ") {\n" + strings.Join(fields, "\n") + "\n}"

	// A batch can partially fail (one item's error doesn't necessarily blank
	// the others' results) — execute still populates resp with whatever
	// succeeded even when it also returns an error, and callers must persist
	// those before propagating the error, or a retry will re-create items
	// that already landed on the board.
	var resp map[string]struct {
		ID string `json:"id"`
	}
	execErr := c.execute(ctx, query, variables, &resp)

	createdIDs := make([]string, len(creates))
	for i := range creates {
		createdIDs[i] = resp[fmt.Sprintf("c%d", i)].ID
	}
	return createdIDs, execErr
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (c *Client) execute(ctx context.Context, query string, variables map[string]any, out any) error {
	body, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// monday's API expects the raw token, no "Bearer " prefix.
	req.Header.Set("Authorization", c.apiToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("monday request: %w", err)
	}
	defer resp.Body.Close()

	var raw struct {
		Data   json.RawMessage `json:"data"`
		Errors []graphQLError  `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return fmt.Errorf("decode monday response: %w", err)
	}

	// Populate out from whatever data came back BEFORE returning any
	// errors — a batch of aliased mutations can partially succeed, and
	// callers need those partial results even when this returns an error.
	var unmarshalErr error
	if out != nil && raw.Data != nil {
		unmarshalErr = json.Unmarshal(raw.Data, out)
	}

	if len(raw.Errors) > 0 {
		msgs := make([]string, len(raw.Errors))
		for i, e := range raw.Errors {
			msgs[i] = e.Message
		}
		return fmt.Errorf("monday API error: %s", strings.Join(msgs, "; "))
	}
	return unmarshalErr
}
