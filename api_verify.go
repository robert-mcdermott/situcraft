package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"situcraft/internal/jobs"
)

// Certificates verified in SituCraft itself (wave 8, slice 9): a certificate, a report or a result
// package given to the page is checked as "situcraft verify" checks it (verify.go), as a job: each
// step made again in a SituCraft of its own from the bundles this library knows (each project's
// datasets' folders and the examples'), the page's records computed again in a headless browser.
// What it found is kept beside the file it was given and answered when the job is done.
//
//	POST /api/verify?name=<file name>  the file's bytes; or JSON {path} (a file on this computer)
//	GET  /api/verify/{job}             what it found (verify's --report), once the job is done

func (a *app) registerVerify(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/verify", a.startVerify)
	mux.HandleFunc("GET /api/verify/{job}", a.verification)
}

func (a *app) startVerify(w http.ResponseWriter, r *http.Request) {
	if a.cacheDir == "" {
		writeError(w, http.StatusServiceUnavailable, "SituCraft has no cache folder to verify in.")
		return
	}
	dir, err := os.MkdirTemp(a.cacheDir, "verify-")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fail := func(code int, message string) {
		os.RemoveAll(dir)
		writeError(w, code, message)
	}
	var file, name string
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var body struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || !filepath.IsAbs(body.Path) {
			fail(http.StatusBadRequest, `give "path": the absolute path of a certificate, report or package`)
			return
		}
		if _, err := os.Stat(body.Path); err != nil {
			fail(http.StatusBadRequest, fmt.Sprintf("%s: not found", body.Path))
			return
		}
		file, name = body.Path, filepath.Base(body.Path)
	} else {
		name = filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = "certificate.zip"
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<30+1))
		if err != nil || len(data) == 0 || len(data) > 1<<30 {
			fail(http.StatusBadRequest, "send the certificate's, report's or package's bytes (at most 1 GB)")
			return
		}
		file = filepath.Join(dir, name)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			fail(http.StatusInternalServerError, err.Error())
			return
		}
	}
	// Checked now, so that a file that is none of them is refused at once.
	if _, _, _, err := readCertificateZip(file); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	folders := a.knownBundleFolders()
	job, _, err := a.jobs.Submit("verify", "Verify "+name, "", map[string]any{"file": name, "folders": len(folders)}, func(ctx context.Context, run *jobs.Run) error {
		run.Progress(0, "Finding its bundles and making its steps again")
		report, err := verifyCertificate(ctx, verifyOptions{certificate: file, data: folders, dev: a.dev, timeout: 6 * time.Hour}, &jobLog{run: run})
		if err != nil {
			return err
		}
		data, _ := json.MarshalIndent(report, "", " ")
		if err := os.WriteFile(filepath.Join(dir, "verification.json"), append(data, '\n'), 0o644); err != nil {
			return err
		}
		run.Progress(1, verdictSentence(report))
		return nil
	})
	if err != nil {
		fail(http.StatusServiceUnavailable, err.Error())
		return
	}
	a.verifyMu.Lock()
	if a.verifications == nil {
		a.verifications = map[string]string{}
	}
	a.verifications[job.ID] = dir
	a.verifyMu.Unlock()
	writeJSON(w, map[string]any{"job": job, "folders": len(folders)})
}

func (a *app) verification(w http.ResponseWriter, r *http.Request) {
	a.verifyMu.Lock()
	dir, ok := a.verifications[r.PathValue("job")]
	a.verifyMu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "No verification of that job.")
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		writeError(w, http.StatusNotFound, "The verification is not done.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// knownBundleFolders: the folders of the bundles this library knows, where a certificate's are
// looked for: each project's datasets' (a zipped bundle's folder is not one), and the examples'.
func (a *app) knownBundleFolders() []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		if info, err := os.Stat(filepath.Join(path, "experiment.xenium")); err == nil && !info.IsDir() {
			seen[path] = true
			out = append(out, path)
		}
	}
	if a.store != nil {
		files, _ := filepath.Glob(filepath.Join(a.store.dir, "projects", "*.json"))
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			var p struct {
				Datasets []struct {
					Path string `json:"path"`
				} `json:"datasets"`
			}
			if json.Unmarshal(data, &p) == nil {
				for _, d := range p.Datasets {
					add(d.Path)
				}
			}
		}
	}
	if a.examples != nil {
		entries, _ := os.ReadDir(a.examples.Dir)
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(a.examples.Dir, e.Name())
			add(dir)
			// A generated cohort's sections, a folder each.
			inner, _ := os.ReadDir(dir)
			for _, s := range inner {
				if s.IsDir() {
					add(filepath.Join(dir, s.Name()))
				}
			}
		}
	}
	return out
}

// jobLog writes what verify prints to the job's log, a line at a time.
type jobLog struct {
	run  *jobs.Run
	line bytes.Buffer
}

func (l *jobLog) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' {
			l.run.Log("info", "%s", l.line.String())
			l.line.Reset()
			continue
		}
		l.line.WriteByte(b)
	}
	return len(p), nil
}

// verdictSentence: what a verification found, in a few words.
func verdictSentence(r *verifyReport) string {
	switch r.Verdict {
	case stepVerified:
		return "Verified: every step made again gives the files certified"
	case stepDiffers:
		if f := r.FirstDiffer; f != nil {
			return fmt.Sprintf("Differs: first at step %d, %s", f.N, f.Name)
		}
		return "Differs"
	}
	return "Not checkable: " + uncheckedText(r)
}
