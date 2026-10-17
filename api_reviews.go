package main

// Reviews of cells and regions are kept in the project (web/lib/review-record.js). A review names
// each cell by its row in its segmentation and by its ID, which the page has not got: the program
// answers the IDs of rows, or the rows of IDs, of the dataset's own cells or of another
// segmentation's.

import (
	"encoding/json"
	"net/http"
)

func (a *app) registerReviews(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/local/{index}/cells/lookup", a.lookupOwnCells)
	mux.HandleFunc("POST /api/local/{index}/segmentations/{id}/cells/lookup", a.lookupSegCells)
}

// cellLookup is a lookup's question and answer: rows, or IDs.
type cellLookup struct {
	Rows []int    `json:"rows"`
	IDs  []string `json:"ids"`
}

func (a *app) lookupOwnCells(w http.ResponseWriter, r *http.Request) {
	d, ok := a.dataset(w, r)
	if !ok {
		return
	}
	lookupCells(w, r, d.Cells.IDs, d.CellRow)
}

func (a *app) lookupSegCells(w http.ResponseWriter, r *http.Request) {
	_, v, ok := a.segViewOf(w, r)
	if !ok {
		return
	}
	ids := make([]string, len(v.cells))
	rows := make(map[string]int, len(v.cells))
	for i, c := range v.cells {
		ids[i] = c.ID
		rows[c.ID] = i
	}
	lookupCells(w, r, ids, func(id string) (int, bool) {
		row, ok := rows[id]
		return row, ok
	})
}

// lookupCells answers the IDs of the rows asked, or the rows of the IDs (−1, or "", for a cell there
// is not).
func lookupCells(w http.ResponseWriter, r *http.Request, ids []string, rowOf func(string) (int, bool)) {
	var q cellLookup
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<20)).Decode(&q); err != nil {
		writeError(w, http.StatusBadRequest, "Ask for {\"rows\": [...]} or {\"ids\": [...]}.")
		return
	}
	if (q.Rows == nil) == (q.IDs == nil) {
		writeError(w, http.StatusBadRequest, "Ask for the IDs of rows, or the rows of IDs: one of the two.")
		return
	}
	out := cellLookup{Rows: []int{}, IDs: []string{}}
	if q.Rows != nil {
		for _, row := range q.Rows {
			id := ""
			if row >= 0 && row < len(ids) {
				id = ids[row]
			}
			out.Rows = append(out.Rows, row)
			out.IDs = append(out.IDs, id)
		}
	} else {
		for _, id := range q.IDs {
			row, ok := rowOf(id)
			if !ok {
				row = -1
			}
			out.Rows = append(out.Rows, row)
			out.IDs = append(out.IDs, id)
		}
	}
	writeJSON(w, out)
}
