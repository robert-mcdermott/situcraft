// SituCraft: a local-first workbench for imaging-based spatial transcriptomics. This program
// serves the embedded web app on the loopback interface and opens it as a desktop window; later
// waves add the data plane (bundle readers, the derived-data cache) and compute (segmentation).
// Adapted from MaveScape 0.8.0 main.go.

package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"situcraft/internal/cache"
	"situcraft/internal/jobs"
)

//go:embed web/index.html web/styles.css web/app.js web/favicon.svg web/lib/*.js web/ui/*.js
var content embed.FS

var version = "0.1.0"

// commit is the source revision the program was built from. Release builds set it with
// -ldflags "-X main.commit=..."; otherwise it comes from the version-control stamp Go records
// when building in a checkout. Methods paragraphs and exports report it.
var commit = ""

// The default port and the 19 after it: 8840–8859. The sibling programs use 8765–8839
// (Proteoscope 8765–8814, CytoWeave 8770–8789, MaveScape 8820–8839); TestPortsStayClearOfTheSiblings
// keeps it that way.
const (
	defaultPort = 8840
	portTries   = 20
)

type config struct {
	host        string
	port        int
	window      string
	keepRunning bool
	dataDir     string
	cacheDir    string
	cacheLimit  int64
	dev         bool
	showVersion bool
	files       []string
}

type app struct {
	session  string
	dev      bool
	assetDir string
	assets   fs.FS
	files    *localFiles
	store    *store
	// cacheDir holds derived data: zipped bundles' unpacked members, and the cache of tile
	// pyramids and spatial indexes (nil when there is no cache folder, as in some tests).
	cacheDir string
	cache    *cache.Cache
	// jobs runs long computations, such as building a dataset's derived data.
	jobs *jobs.Manager
}

func init() {
	if err := mime.AddExtensionType(".js", "text/javascript; charset=utf-8"); err != nil {
		log.Printf("mime registration warning: %v", err)
	}
	if err := mime.AddExtensionType(".mjs", "text/javascript; charset=utf-8"); err != nil {
		log.Printf("mime registration warning: %v", err)
	}
}

func main() {
	cfg, err := parseConfig(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		os.Exit(2)
	}
	if cfg.showVersion {
		// One line, "situcraft X.Y.Z": the installers check exactly this. The commit is in
		// /api/info and the status bar.
		fmt.Printf("situcraft %s\n", version)
		return
	}
	// A SituCraft already running on the preferred port gets the paths and a new window, so that
	// one program owns the library, the cache and any running jobs.
	if existing := findRunning(cfg.host, cfg.port); existing != "" {
		handed := false
		if len(cfg.files) > 0 {
			if err := forwardFiles(existing, cfg.files); err != nil {
				log.Printf("could not pass the paths to the running SituCraft: %v", err)
			} else {
				handed = true
			}
		}
		if handed {
			names := make([]string, 0, len(cfg.files))
			for _, f := range cfg.files {
				names = append(names, filepath.Base(f))
			}
			fmt.Printf("SituCraft is already running at %s: %s handed to it.\nSwitch to its window (they open when it comes to the front); stop it with Ctrl+C in the terminal that started it.\n", existing, strings.Join(names, ", "))
		} else {
			fmt.Printf("SituCraft is already running at %s; stop it with Ctrl+C in the terminal that started it.\n", existing)
		}
		if cfg.window != "none" {
			if _, err := openWindow(existing, cfg.window, cfg.dataDir); err != nil {
				log.Printf("open window: %v", err)
			}
		}
		return
	}
	running, err := start(cfg, os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- running.serve() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-done:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	case <-signals:
		running.stop(3 * time.Second)
	case <-running.windowClosed:
		fmt.Println("The SituCraft window was closed; stopping.")
		running.stop(3 * time.Second)
	}
}

// buildCommit is the revision the program was built from: the -X value, else the VCS stamp
// (with "-dirty" when the checkout had uncommitted changes), else "".
func buildCommit() string {
	if commit != "" {
		return commit
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision != "" && modified {
		revision += "-dirty"
	}
	return revision
}

// A prepared server: the app, its listener and address.
type runningServer struct {
	app          *app
	server       *http.Server
	listener     net.Listener
	url          string
	cancel       context.CancelFunc
	windowClosed <-chan struct{}
}

func (s *runningServer) stop(timeout time.Duration) {
	// A running job stops where it is (what it finished is kept and resumed next time); a job
	// slow to notice is not waited for beyond the timeout.
	closed := make(chan struct{})
	go func() {
		s.app.jobs.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(timeout):
	}
	s.cancel()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s.server.Shutdown(ctx)
}

func (s *runningServer) serve() error {
	if err := s.server.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// start prepares the app, listens, prints the banner to out and opens the window.
func start(cfg config, out io.Writer) (*runningServer, error) {
	a, err := newApp(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare app: %w", err)
	}
	handler, err := a.handler()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare app: %w", err)
	}
	listener, actualPort, err := listen(cfg.host, cfg.port)
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}
	url := "http://" + net.JoinHostPort(cfg.host, strconv.Itoa(actualPort))
	a.printBanner(out, url)
	base, cancel := context.WithCancel(context.Background())
	server := &http.Server{
		Handler:           protect(handler, cfg.host, actualPort),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return base },
	}
	running := &runningServer{app: a, server: server, listener: listener, url: url, cancel: cancel}
	if cfg.window != "none" {
		closed := make(chan struct{})
		running.windowClosed = closed
		go func() {
			time.Sleep(250 * time.Millisecond)
			wait, err := openWindow(url, cfg.window, cfg.dataDir)
			if err != nil {
				log.Printf("open window: %v", err)
				return
			}
			// An app window with its own browser profile tells us when it closes; the
			// default browser does not, so the server keeps running until Ctrl+C.
			if wait != nil && !cfg.keepRunning {
				opened := time.Now()
				wait()
				// A browser already running with this profile (on macOS, Chrome keeps
				// running after its last window closes) takes the window over and the
				// process started here exits at once: that is not the window closing.
				if time.Since(opened) < handOffTime {
					fmt.Fprintln(out, "The window opened in a SituCraft browser that was already running; SituCraft keeps serving until you press Ctrl+C (or quit that browser with Cmd+Q and start again).")
					return
				}
				close(closed)
			}
		}()
	}
	return running, nil
}

// A window process that exits sooner than this handed the window to a browser already running.
const handOffTime = 5 * time.Second

func parseConfig(args []string) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("situcraft", flag.ContinueOnError)
	flags.StringVar(&cfg.host, "host", "127.0.0.1", "host interface to bind")
	flags.IntVar(&cfg.port, "port", defaultPort, "preferred localhost port")
	flags.StringVar(&cfg.window, "window", "app", "how to show SituCraft: app (a desktop window from Chrome, Edge, Brave or Chromium, else the default browser), browser (the default browser) or none")
	flags.BoolVar(&cfg.keepRunning, "keep-running", false, "keep serving after the app window is closed")
	flags.StringVar(&cfg.dataDir, "data-dir", "", "folder for the project library (default: <user config dir>/SituCraft)")
	flags.StringVar(&cfg.cacheDir, "cache-dir", "", "folder for derived data, which can be deleted and made again (default: <user cache dir>/situcraft)")
	cacheLimit := flags.String("cache-limit", "10GB", "size the cache is kept within, removing what was used least recently (for example 500MB or 20GB; 0 for no limit)")
	flags.BoolVar(&cfg.dev, "dev", false, "serve web/ from the working directory instead of the embedded copy")
	flags.BoolVar(&cfg.showVersion, "version", false, "print the version and exit")
	noOpen := flags.Bool("no-open", false, "same as --window none")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: situcraft [flags] [Xenium output bundles (folders or .zip), images (.ome.tif), tables (.parquet, .csv), polygons (.geojson), Zarr stores, projects (.scz) or folders of them...]")
		flags.PrintDefaults()
	}
	files, err := parseArgs(flags, args)
	if *noOpen {
		cfg.window = "none"
	}
	if err == nil {
		if cfg.cacheLimit, err = cache.ParseSize(*cacheLimit); err != nil {
			fmt.Fprintf(flags.Output(), "--cache-limit: %v\n", err)
		}
	}
	switch cfg.window {
	case "app", "browser", "none":
	default:
		if err == nil {
			fmt.Fprintf(flags.Output(), "--window must be app, browser or none, not %q\n", cfg.window)
			err = errors.New("invalid --window")
		}
	}
	if cfg.dataDir == "" && err == nil {
		cfg.dataDir = defaultDataDir()
	}
	if cfg.cacheDir == "" && err == nil {
		cfg.cacheDir = defaultCacheDir()
	}
	cfg.files = files
	return cfg, err
}

// parseArgs lets flags follow paths, as in "situcraft bundle/ --port 9000".
func parseArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

func newApp(cfg config) (*app, error) {
	a := &app{
		session:  strconv.FormatInt(time.Now().UnixNano(), 36),
		dev:      cfg.dev,
		assets:   content,
		files:    newLocalFiles(),
		cacheDir: cfg.cacheDir,
		jobs:     jobs.NewManager(),
	}
	if a.cacheDir != "" {
		c, err := cache.Open(a.cacheDir, cfg.cacheLimit)
		if err != nil {
			return nil, fmt.Errorf("the cache folder (%s): %w", a.cacheDir, err)
		}
		a.cache = c
		cache.SetProgram("situcraft " + version)
		// What was left over by earlier runs is trimmed to the limit.
		go func() {
			removed, err := c.Evict()
			if err != nil {
				log.Printf("trimming the cache: %v", err)
			}
			for _, item := range removed {
				log.Printf("removed %s from the cache (%d bytes), to stay within its limit", item.Name, item.Size)
			}
		}()
	}
	if problems := a.files.add(cfg.files); len(problems) > 0 {
		for _, problem := range problems {
			log.Print(problem)
		}
	}
	// Unlike its siblings, SituCraft always keeps its library on disk: projects refer to bundles
	// by their place on this computer, which a browser's own storage cannot do.
	s, err := openStore(cfg.dataDir)
	if err != nil {
		return nil, fmt.Errorf("the project library (%s): %w", cfg.dataDir, err)
	}
	a.store = s
	if !cfg.dev {
		return a, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	a.assetDir = dir
	a.assets = os.DirFS(dir)
	if _, err := fs.Stat(a.assets, "web/index.html"); err != nil {
		return nil, fmt.Errorf("--dev must be run from the repository root: %w", err)
	}
	return a, nil
}

func (a *app) printBanner(out io.Writer, url string) {
	fmt.Fprintf(out, "SituCraft %s is running at %s\n", version, url)
	if a.dev {
		fmt.Fprintf(out, "Dev mode: serving web/ from disk in %s (no-store)\n", a.assetDir)
	}
	fmt.Fprintf(out, "Project library: %s\n", a.store.dir)
	if a.cache != nil && a.cache.Limit() > 0 {
		fmt.Fprintf(out, "Cache: %s (kept within %s)\n", a.cacheDir, formatSize(a.cache.Limit()))
	} else {
		fmt.Fprintf(out, "Cache: %s\n", a.cacheDir)
	}
	if n := len(a.files.list()); n > 0 {
		fmt.Fprintf(out, "Opening %d item(s) named on the command line\n", n)
	}
	fmt.Fprintln(out, "Press Ctrl+C to stop.")
}

func (a *app) handler() (http.Handler, error) {
	webFS, err := fs.Sub(a.assets, "web")
	if err != nil {
		return nil, err
	}
	webCache := "no-cache"
	if a.dev {
		webCache = "no-store"
	}
	mux := http.NewServeMux()
	a.registerAPI(mux)
	mux.Handle("/", cacheControl(webCache, staticHandler(webFS)))
	return mux, nil
}

type info struct {
	Name     string `json:"name"`
	Session  string `json:"session"`
	Version  string `json:"version"`
	Commit   string `json:"commit,omitempty"`
	Mode     string `json:"mode"`
	DataDir  string `json:"dataDir"`
	CacheDir string `json:"cacheDir"`
	// CacheLimit is the size the cache is kept within, in bytes (0: no limit).
	CacheLimit int64       `json:"cacheLimit"`
	Files      []localFile `json:"files"`
	Platform   string      `json:"platform"`
}

func (a *app) registerAPI(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		limit := int64(0)
		if a.cache != nil {
			limit = a.cache.Limit()
		}
		writeJSON(w, info{Name: "SituCraft", Session: a.session, Version: version, Commit: buildCommit(), Mode: "desktop", DataDir: a.store.dir, CacheDir: a.cacheDir, CacheLimit: limit, Files: a.files.list(), Platform: platformName()})
	})
	mux.HandleFunc("GET /api/local/{index}", a.files.serve)
	mux.HandleFunc("POST /api/open", a.openPaths)
	a.store.register(mux)
	a.registerDatasets(mux)
	a.registerCache(mux)
	a.registerJobs(mux)
	mux.HandleFunc("POST /api/library/local/{index}", func(w http.ResponseWriter, r *http.Request) {
		file, ok := a.files.get(r.PathValue("index"))
		if !ok {
			writeError(w, http.StatusNotFound, "No such local file.")
			return
		}
		if file.isDir {
			writeError(w, http.StatusBadRequest, file.Name+" is a folder: SituCraft reads it where it is and does not copy it into the library.")
			return
		}
		a.store.addLocalFile(w, file.path)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Unknown API endpoint.")
	})
}

// openPaths adds paths named by a second "situcraft <paths>" launch; only this computer may ask.
func (a *app) openPaths(w http.ResponseWriter, r *http.Request) {
	if !isLoopbackRequest(r) {
		writeError(w, http.StatusForbidden, "Only this computer may open files.")
		return
	}
	var body struct {
		Paths []string `json:"paths"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Send {\"paths\": [...]}.")
		return
	}
	problems := a.files.add(body.Paths)
	writeJSON(w, map[string]any{"files": a.files.list(), "problems": problems})
}

func listen(host string, port int) (net.Listener, int, error) {
	var lastErr error
	for offset := 0; offset < portTries; offset++ {
		candidate := port + offset
		listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(candidate)))
		if err == nil {
			return listener, listener.Addr().(*net.TCPAddr).Port, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

// findRunning returns the URL of a SituCraft already serving on host:port, or "".
func findRunning(host string, port int) string {
	url := "http://" + net.JoinHostPort(host, strconv.Itoa(port))
	client := http.Client{Timeout: 400 * time.Millisecond}
	response, err := client.Get(url + "/api/info")
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	var body info
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&body) != nil {
		return ""
	}
	if body.Name != "SituCraft" {
		return ""
	}
	return url
}

func forwardFiles(url string, paths []string) error {
	absolute := make([]string, 0, len(paths))
	for _, path := range paths {
		if abs, err := absPath(path); err == nil {
			absolute = append(absolute, abs)
		}
	}
	payload, _ := json.Marshal(map[string]any{"paths": absolute})
	request, err := http.NewRequest(http.MethodPost, url+"/api/open", strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", response.StatusCode)
	}
	return nil
}

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
}

// staticHandler serves the web app; unknown paths without an extension get index.html.
func staticHandler(webFS fs.FS) http.Handler {
	files := http.FileServer(http.FS(webFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			files.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(webFS, name); err != nil {
			if !strings.Contains(name[strings.LastIndex(name, "/")+1:], ".") {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				files.ServeHTTP(w, r2)
				return
			}
			writeError(w, http.StatusNotFound, "Not found.")
			return
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
