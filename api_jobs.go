package main

// Jobs (internal/jobs) over HTTP: the list, one job with its log, cancelling, resuming an
// interrupted job and discarding a finished one, and a stream of server-sent events that sends the
// list whenever a job changes, so the page follows progress without polling and finds running jobs
// again after a reload. handleJobs tells the manager how each kind of job is resumed (from its
// spec) and what discarding one clears away.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"situcraft/internal/compare"
	"situcraft/internal/jobs"
	"situcraft/internal/label"
	"situcraft/internal/xenium"
)

// jobLogLines is how many of each job's last log lines the list and the stream carry.
const jobLogLines = 5

func (a *app) registerJobs(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"jobs": a.jobs.List(jobLogLines)})
	})
	mux.HandleFunc("GET /api/jobs/events", a.jobEvents)
	mux.HandleFunc("GET /api/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
		job, ok := a.jobs.Get(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "No such job.")
			return
		}
		writeJSON(w, job)
	})
	mux.HandleFunc("POST /api/jobs/{id}/resume", func(w http.ResponseWriter, r *http.Request) {
		job, err := a.jobs.Resume(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, job)
	})
	mux.HandleFunc("POST /api/jobs/{id}/discard", func(w http.ResponseWriter, r *http.Request) {
		if err := a.jobs.Discard(r.PathValue("id")); err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"discarded": true})
	})
	mux.HandleFunc("POST /api/jobs/{id}/cancel", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := a.jobs.Get(id); !ok {
			writeError(w, http.StatusNotFound, "No such job.")
			return
		}
		writeJSON(w, map[string]bool{"cancelled": a.jobs.Cancel(id)})
	})
}

// Changes closer together than this are sent as one event.
const jobEventInterval = 200 * time.Millisecond

// jobEvents streams "jobs" events, each the whole list, at most every jobEventInterval, with a
// comment now and then to keep the connection open. It ends when the page goes away or the
// program stops.
func (a *app) jobEvents(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	header := w.Header()
	header.Set("Content-Type", "text/event-stream")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Accel-Buffering", "no")
	changes, stop := a.jobs.Subscribe()
	defer stop()
	send := func() bool {
		raw, err := json.Marshal(a.jobs.List(jobLogLines))
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: jobs\ndata: %s\n\n", raw); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send() {
		return
	}
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		case <-changes:
			// Let a burst of progress settle into one event.
			select {
			case <-time.After(jobEventInterval):
			case <-r.Context().Done():
				return
			}
			select {
			case <-changes:
			default:
			}
			if !send() {
				return
			}
		}
	}
}

// handleJobs registers how each kind of job is resumed from its spec, and what discarding one
// clears away.
func (a *app) handleJobs() {
	bundleOf := func(spec json.RawMessage) (string, error) {
		var p struct {
			Bundle string `json:"bundle"`
		}
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" {
			return "", errors.New("its spec names no bundle")
		}
		return p.Bundle, nil
	}
	a.jobs.Handle("cache", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		path, err := bundleOf(spec)
		if err != nil {
			return nil, err
		}
		// What the interrupted build finished is kept; the build continues from there.
		return a.cacheJob(path), nil
	}, nil)
	a.jobs.Handle("segmentation", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		path, err := bundleOf(spec)
		if err != nil {
			return nil, err
		}
		return a.segmentationJob(path), nil
	}, nil)
	a.jobs.Handle("expansion", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		p, err := expansionOf(spec)
		if err != nil {
			return nil, err
		}
		// An expansion starts again: it takes seconds, not hours.
		return a.expansionJob(p.Bundle, p.DistanceUm), nil
	}, func(id string, spec json.RawMessage) {
		// Its staged files go with it.
		p, err := expansionOf(spec)
		if err != nil {
			return
		}
		if b, err := xenium.Open(p.Bundle, xenium.Options{CacheDir: a.cacheDir}); err == nil {
			if instrument := a.knownSegmentation(b); instrument != nil {
				a.artifacts.Discard(expansionID(instrument.ID, p.DistanceUm))
			}
			b.Close()
		}
	})
	a.jobs.Handle("refinement", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		p, err := refineOf(spec)
		if err != nil {
			return nil, err
		}
		// A refinement starts again from its seed: the same seed gives the same segmentation.
		return a.refineJob(p), nil
	}, func(id string, spec json.RawMessage) {
		// Its staged files go with it.
		p, err := refineOf(spec)
		if err != nil {
			return
		}
		if b, err := xenium.Open(p.Bundle, xenium.Options{CacheDir: a.cacheDir}); err == nil {
			if instrument := a.knownSegmentation(b); instrument != nil {
				a.artifacts.Discard(refineID(instrument.ID, p.Spec))
			}
			b.Close()
		}
	})
	a.jobs.Handle("import", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p importSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" {
			return nil, errors.New("its spec names no bundle")
		}
		if err := checkImport(&p.Options); err != nil {
			return nil, err
		}
		// An import starts again: it reads its files anew.
		return a.importJob(p.Bundle, p.Options), nil
	}, func(id string, spec json.RawMessage) {
		a.artifacts.Discard("imp-job-" + strings.ToLower(id))
	})
	a.jobs.Handle("comparison", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p compareSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.A == "" || p.B == "" {
			return nil, errors.New("its spec names no bundle and segmentations")
		}
		return a.comparisonJob(p.Bundle, p.A, p.B), nil
	}, func(id string, spec json.RawMessage) {
		var p compareSpec
		if json.Unmarshal(spec, &p) == nil && p.A != "" && p.B != "" {
			a.artifacts.Discard(comparisonID(p.A, p.B, compare.DefaultParams()))
		}
	})
	a.jobs.Handle("clustering", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p clusterSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Segmentation == "" || p.Parameters.Check() != nil {
			return nil, errors.New("its spec names no bundle, segmentation and parameters")
		}
		return a.clusteringJob(p, clusteringID(p.Segmentation, p.Filter, p.Parameters)), nil
	}, func(id string, spec json.RawMessage) {
		var p clusterSpec
		if json.Unmarshal(spec, &p) == nil && p.Segmentation != "" {
			a.artifacts.Discard(clusteringID(p.Segmentation, p.Filter, p.Parameters))
		}
	})
	a.jobs.Handle("embedding", func(spec json.RawMessage) (jobs.Func, error) {
		var p embedSpec
		if json.Unmarshal(spec, &p) != nil || p.Clustering == "" || p.Parameters.Check() != nil {
			return nil, errors.New("its spec names no clustering and parameters")
		}
		return a.embeddingJob(p, embeddingID(p.Clustering, p.Parameters)), nil
	}, func(id string, spec json.RawMessage) {
		var p embedSpec
		if json.Unmarshal(spec, &p) == nil && p.Clustering != "" {
			a.artifacts.Discard(embeddingID(p.Clustering, p.Parameters))
		}
	})
	labelsOf := func(spec json.RawMessage) (labelSpec, string, error) {
		var p labelSpec
		if json.Unmarshal(spec, &p) != nil || p.Clustering == "" || p.Parameters.Check() != nil {
			return p, "", errors.New("its spec names no clustering, marker set and parameters")
		}
		set, err := label.Parse(p.Markers)
		if err != nil {
			return p, "", err
		}
		return p, labelsID(p.Clustering, set.Checksum(), p.Parameters), nil
	}
	a.jobs.Handle("labels", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		p, id, err := labelsOf(spec)
		if err != nil {
			return nil, err
		}
		return a.labelsJob(p, id), nil
	}, func(id string, spec json.RawMessage) {
		if _, lid, err := labelsOf(spec); err == nil {
			a.artifacts.Discard(lid)
		}
	})
	a.jobs.Handle("label-import", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p importLabelsSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Ask.Segmentation == "" || p.SHA256 == "" {
			return nil, errors.New("its spec names no bundle, segmentation and table")
		}
		return a.labelImportJob(p, importedLabelsID(p.Ask.Segmentation, p.SHA256, p.Ask.Columns)), nil
	}, func(id string, spec json.RawMessage) {
		var p importLabelsSpec
		if json.Unmarshal(spec, &p) == nil && p.SHA256 != "" {
			a.artifacts.Discard(importedLabelsID(p.Ask.Segmentation, p.SHA256, p.Ask.Columns))
		}
	})
	a.jobs.Handle("label-reference", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		p, id, err := a.referenceJobOf(spec)
		if err != nil {
			return nil, err
		}
		return a.referenceJob(p, id), nil
	}, func(id string, spec json.RawMessage) {
		if _, lid, err := a.referenceJobOf(spec); err == nil {
			a.artifacts.Discard(lid)
		}
	})
	a.jobs.Handle("analysis", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p analysisSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Clustering == "" {
			return nil, errors.New("its spec names no bundle and clustering")
		}
		return a.analysisJob(p), nil
	}, func(string, json.RawMessage) {})
	a.jobs.Handle("plant", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p plantSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || checkPlant(p.Params) != nil {
			return nil, errors.New("its spec names no bundle and parameters")
		}
		return a.plantJob(p.Bundle, p.Params), nil
	}, func(id string, spec json.RawMessage) {
		var p plantSpec
		if json.Unmarshal(spec, &p) == nil && p.Bundle != "" {
			if b, err := xenium.Open(p.Bundle, xenium.Options{CacheDir: a.cacheDir}); err == nil {
				if instrument := a.knownSegmentation(b); instrument != nil {
					a.artifacts.Discard(plantID(instrument.ID, p.Params))
				}
				b.Close()
			}
		}
	})
	a.jobs.Handle("export", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		p, err := exportOf(spec)
		if err != nil {
			return nil, err
		}
		// An export starts again; what the interrupted one left beside its folder goes first.
		removePartialExports(p.Folder)
		return a.exportJob(p), nil
	}, func(id string, spec json.RawMessage) {
		if p, err := exportOf(spec); err == nil {
			removePartialExports(p.Folder)
		}
	})
	// A cohort's summary and a comparison are made again from their specifications.
	a.jobs.Handle("cohort", func(spec json.RawMessage) (jobs.Func, error) {
		var p cohortSummarySpec
		if err := json.Unmarshal(spec, &p); err != nil || len(p.Bundles) != len(p.Samples) {
			return nil, errors.New("the summary's specification is not readable")
		}
		return a.cohortSummaryJob(p, cohortSummaryID(p.cohortSummaryRequest)), nil
	}, func(id string, spec json.RawMessage) {
		var p cohortSummarySpec
		if json.Unmarshal(spec, &p) == nil && len(p.Samples) > 0 {
			a.artifacts.Discard(cohortSummaryID(p.cohortSummaryRequest))
		}
	})
	a.jobs.Handle("abundance", func(spec json.RawMessage) (jobs.Func, error) {
		var p abundanceRequest
		if err := json.Unmarshal(spec, &p); err != nil || p.Summary == "" {
			return nil, errors.New("the comparison's specification is not readable")
		}
		return a.abundanceJob(p, abundanceID(p)), nil
	}, func(id string, spec json.RawMessage) {
		var p abundanceRequest
		if json.Unmarshal(spec, &p) == nil && p.Summary != "" {
			a.artifacts.Discard(abundanceID(p))
		}
	})
	a.jobs.Handle("expression", func(spec json.RawMessage) (jobs.Func, error) {
		var p expressionRequest
		if err := json.Unmarshal(spec, &p); err != nil || p.Summary == "" {
			return nil, errors.New("the comparison's specification is not readable")
		}
		return a.expressionJob(p, expressionID(p)), nil
	}, func(id string, spec json.RawMessage) {
		var p expressionRequest
		if json.Unmarshal(spec, &p) == nil && p.Summary != "" {
			a.artifacts.Discard(expressionID(p))
		}
	})
	a.jobs.Handle("spatial-tests", func(spec json.RawMessage) (jobs.Func, error) {
		var p spatialTestsRequest
		if err := json.Unmarshal(spec, &p); err != nil || p.Summary == "" {
			return nil, errors.New("the comparison's specification is not readable")
		}
		return a.spatialTestsJob(p, spatialTestsID(p)), nil
	}, func(id string, spec json.RawMessage) {
		var p spatialTestsRequest
		if json.Unmarshal(spec, &p) == nil && p.Summary != "" {
			a.artifacts.Discard(spatialTestsID(p))
		}
	})
	a.jobs.Handle("example", func(spec json.RawMessage) (jobs.Func, error) {
		var p struct {
			Example string `json:"example"`
		}
		if json.Unmarshal(spec, &p) != nil || p.Example == "" {
			return nil, errors.New("its spec names no example")
		}
		return a.exampleJob(p.Example)
	}, func(id string, spec json.RawMessage) {
		// A download interrupted by a crash leaves its partial files; they go with it.
		var p struct {
			Example string `json:"example"`
		}
		if json.Unmarshal(spec, &p) == nil && p.Example != "" {
			a.examples.Clean(p.Example)
		}
	})
	// A graph and a spatial analysis are named by what they are made from, as when they were
	// asked for; one discarded clears away what it staged.
	a.jobs.Handle("graph", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p graphSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Segmentation == "" || p.Parameters.Check() != nil {
			return nil, errors.New("its spec names no bundle, segmentation and parameters")
		}
		return a.graphJob(p, graphID(p.Segmentation, p.Cells.Checksum, p.Parameters)), nil
	}, func(id string, spec json.RawMessage) {
		var p graphSpec
		if json.Unmarshal(spec, &p) == nil && p.Segmentation != "" {
			a.artifacts.Discard(graphID(p.Segmentation, p.Cells.Checksum, p.Parameters))
		}
	})
	a.jobs.Handle("spatial", func(spec json.RawMessage) (jobs.Func, error) {
		if a.cache == nil {
			return nil, errors.New("SituCraft has no cache folder")
		}
		var p spatialSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Segmentation == "" || p.Question == "" {
			return nil, errors.New("its spec names no bundle, segmentation and question")
		}
		params, err := checkedParameters(p.Question, p.Parameters)
		if err != nil {
			return nil, err
		}
		return a.spatialJob(p, spatialID(p.spatialRequest, params)), nil
	}, func(id string, spec json.RawMessage) {
		var p spatialSpec
		if json.Unmarshal(spec, &p) == nil && p.Question != "" {
			if params, err := checkedParameters(p.Question, p.Parameters); err == nil {
				a.artifacts.Discard(spatialID(p.spatialRequest, params))
			}
		}
	})
	// An image, a registration and an alignment's check are named by what they find (the image's
	// checksum, the placement, the check's numbers), so what an interrupted one staged is not
	// known from its spec: they are made again, and nothing is cleared away.
	a.jobs.Handle("image", func(spec json.RawMessage) (jobs.Func, error) {
		var p imageSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Path == "" {
			return nil, errors.New("its spec names no bundle and image")
		}
		return a.imageJob(p), nil
	}, nil)
	a.jobs.Handle("register", func(spec json.RawMessage) (jobs.Func, error) {
		var p registerSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Image == "" {
			return nil, errors.New("its spec names no bundle and image")
		}
		return a.registerJob(p), nil
	}, nil)
	a.jobs.Handle("check", func(spec json.RawMessage) (jobs.Func, error) {
		var p checkSpec
		if json.Unmarshal(spec, &p) != nil || p.Bundle == "" || p.Image == "" || p.Transform == "" {
			return nil, errors.New("its spec names no bundle, image and alignment")
		}
		return a.checkJob(p), nil
	}, nil)
	// A verification is not resumed: what it was sent is not kept (verify it again).
}
