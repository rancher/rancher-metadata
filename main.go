package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/ghodss/yaml"
	"github.com/golang/gddo/httputil"
	"github.com/gorilla/mux"
)

const (
	ContentText = 1
	ContentJSON = 2
	ContentYAML = 3

	// The top-level key in the JSON for the default (not client-specific answers)
	DEFAULT_KEY = "default"

	// A key to check for magic traversing of arrays by a string field in them
	// For example, given: { things: [ {name: 'asdf', stuff: 42}, {name: 'zxcv', stuff: 43} ] }
	// Both ../things/0/stuff and ../things/asdf/stuff will return 42 because 'asdf' matched the 'anme' field of one of the 'things'.
	MAGIC_ARRAY_KEY = "name"
)

var (
	debug        = flag.Bool("debug", false, "Debug")
	enableXff    = flag.Bool("xff", false, "X-Forwarded-For header support")
	listen       = flag.String("listen", ":80", "Address to listen to (TCP)")
	listenReload = flag.String("listenReload", "127.0.0.1:8112", "Address to listen to for reload requests (TCP)")
	answersFile  = flag.String("answers", "./answers.yaml", "File containing the answers to respond with")
	logFile      = flag.String("log", "", "Log file")
	pidFile      = flag.String("pid-file", "", "PID to write to")

	router  = mux.NewRouter()
	answers Versions

	wantRevision   = 1
	loadedRevision = 0
	loading        = false
	ticker         = time.NewTicker(1 * time.Second)
)

func main() {
	log.Info("Starting rancher-metadata")
	parseFlags()
	err := loadAnswers()
	if err != nil {
		log.Fatal("Cannot startup without a valid Answers file")
	}

	watchSignals()
	watchHttp()

	router.HandleFunc("/favicon.ico", http.NotFound)
	router.HandleFunc("/", root).
		Methods("GET", "HEAD").
		Name("Root")

	router.HandleFunc("/{version}", metadata).
		Methods("GET", "HEAD").
		Name("Version")

	router.HandleFunc("/{version}/{key:.*}", metadata).
		Methods("GET", "HEAD").
		Name("Metadata")

	log.Info("Listening on ", *listen)
	log.Fatal(http.ListenAndServe(*listen, router))
}

func parseFlags() {
	flag.Parse()

	if *debug {
		log.SetLevel(log.DebugLevel)
	}

	if *logFile != "" {
		if output, err := os.OpenFile(*logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err != nil {
			log.Fatalf("Failed to log to file %s: %v", *logFile, err)
		} else {
			log.SetOutput(output)
		}
	}

	if *pidFile != "" {
		log.Infof("Writing pid %d to %s", os.Getpid(), *pidFile)
		if err := ioutil.WriteFile(*pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			log.Fatalf("Failed to write pid file %s: %v", *pidFile, err)
		}
	}
}

func loadAnswers() (err error) {
	log.Debug("Loading answers")
	loading = true
	revision := wantRevision
	neu, err := ParseAnswers(*answersFile)
	if err == nil {
		for _, data := range neu {
			defaults, ok := data[DEFAULT_KEY]
			if ok {
				defaultsMap, ok := defaults.(map[string]interface{})
				if ok {
					// Copy the defaults into the per-client info, so there's no
					// complicated merging and lookup logic when retrieving.
					mergeDefaults(&data, defaultsMap)
				}
			}
		}

		answers = neu
		loadedRevision = revision
		loading = false
		log.Infof("Loaded answers revision %d for %d versions", revision, len(answers))
	} else {
		log.Errorf("Failed to load answers: %v", err)
	}
	return err
}

func mergeDefaults(clientAnswers *Answers, defaultAnswers map[string]interface{}) {
	for _, client := range *clientAnswers {
		clientMap, ok := client.(map[string]interface{})
		if ok {
			for k, v := range defaultAnswers {
				_, exists := clientMap[k]
				if !exists {
					clientMap[k] = v
				}
			}
		}
	}
}

func watchSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for _ = range c {
			log.Info("Received HUP signal")
			wantRevision += 1
		}
	}()

	go func() {
		for {
			select {
			case <-ticker.C:
				//log.Info("Ping")
				if !loading && loadedRevision < wantRevision {
					loadAnswers()
				}
			}
		}
	}()

}

func watchHttp() {
	reloadRouter := mux.NewRouter()
	reloadRouter.HandleFunc("/favicon.ico", http.NotFound)
	reloadRouter.HandleFunc("/v1/reload", httpReload).Methods("POST")

	log.Info("Listening for Reload on ", *listenReload)
	go http.ListenAndServe(*listenReload, reloadRouter)
}

func httpReload(w http.ResponseWriter, req *http.Request) {
	wantRevision += 1
	waitFor := wantRevision
	log.Debugf("Received HTTP reload request, wait for %d, ", waitFor)

	for {
		select {
		case <-time.After(100 * time.Millisecond):
			//log.Debugf("Now at %d, waiting for %d", loadedRevision, waitFor)
			if loadedRevision >= waitFor {
				fmt.Fprintf(w, "OK %d\r\n", loadedRevision)
				return
			}
		}
	}
}

func contentType(req *http.Request) int {
	str := httputil.NegotiateContentType(req, []string{
		"text/plain",
		"application/json",
		"application/yaml",
		"application/x-yaml",
		"text/yaml",
		"text/x-yaml",
	}, "text/plain")

	if strings.Contains(str, "json") {
		return ContentJSON
	} else if strings.Contains(str, "yaml") {
		return ContentYAML
	} else {
		return ContentText
	}
}

func root(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	log.WithFields(log.Fields{"client": requestIp(req), "version": "root"}).Infof("OK: %s", "/")

	m := make(map[string]interface{})
	for _, k := range answers.Versions() {
		url, err := router.Get("Version").URL("version", k)
		if err == nil {
			m[k] = (*url).String()
		} else {
			log.Warn("Error: ", err.Error())
		}
	}

	// If latest isn't in the list, pretend it is
	_, ok := m["latest"]
	if !ok {
		url, err := router.Get("Version").URL("version", "latest")
		if err == nil {
			m["latest"] = (*url).String()
		} else {
			log.Warn("Error: ", err.Error())
		}
	}

	respondSuccess(w, req, m)
}

func metadata(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	vars := mux.Vars(req)
	clientIp := requestIp(req)

	version := vars["version"]
	_, ok := answers[version]
	if !ok {
		// If a `latest` key is not provided, pick the ASCII-betically highest version and call it that.
		if version == "latest" {
			version = ""
			for _, k := range answers.Versions() {
				if k > version {
					version = k
				}
			}

			log.Debugf("Picked %s for latest version because none provided", version)
		} else {
			respondError(w, req, "Invalid version", http.StatusNotFound)
			return
		}
	}

	key := strings.TrimRight(vars["key"], "/")
	displayKey := "/" + key

	log.WithFields(log.Fields{"version": version, "client": clientIp}).Debugf("Searching for: %s", key)
	val, ok := answers.Matching(version, clientIp, key)

	if ok {
		log.WithFields(log.Fields{"version": version, "client": clientIp}).Infof("OK: %s", displayKey)
		respondSuccess(w, req, val)
	} else {
		log.WithFields(log.Fields{"version": version, "client": clientIp}).Infof("Error: %s", displayKey)
		respondError(w, req, "Not found", http.StatusNotFound)
	}
}

func respondError(w http.ResponseWriter, req *http.Request, msg string, statusCode int) {
	obj := make(map[string]interface{})
	obj["message"] = msg
	obj["type"] = "error"
	obj["code"] = statusCode

	switch contentType(req) {
	case ContentText:
		http.Error(w, msg, statusCode)
	case ContentJSON:
		bytes, err := json.Marshal(obj)
		if err == nil {
			http.Error(w, string(bytes), statusCode)
		} else {
			http.Error(w, "{\"type\": \"error\", \"message\": \"JSON marshal error\"}", http.StatusInternalServerError)
		}
	case ContentYAML:
		bytes, err := yaml.Marshal(obj)
		if err == nil {
			http.Error(w, string(bytes), statusCode)
		} else {
			http.Error(w, "type: \"error\"\nmessage: \"JSON marshal error\"", http.StatusInternalServerError)
		}
	}
}

func respondSuccess(w http.ResponseWriter, req *http.Request, val interface{}) {
	switch contentType(req) {
	case ContentText:
		respondText(w, req, val)
	case ContentJSON:
		respondJSON(w, req, val)
	case ContentYAML:
		respondYAML(w, req, val)
	}
}

func respondText(w http.ResponseWriter, req *http.Request, val interface{}) {
	if val == nil {
		fmt.Fprint(w, "")
		return
	}

	switch v := val.(type) {
	case string:
		fmt.Fprint(w, v)
	case uint, uint8, uint16, uint32, uint64, int, int8, int16, int32, int64:
		fmt.Fprintf(w, "%d", v)
	case float64:
		// The default format has extra trailing zeros
		str := strings.TrimRight(fmt.Sprintf("%f", v), "0")
		str = strings.TrimRight(str, ".")
		fmt.Fprint(w, str)
	case bool:
		if v {
			fmt.Fprint(w, "true")
		} else {
			fmt.Fprint(w, "false")
		}
	case map[string]interface{}:
		out := make([]string, len(v))
		i := 0
		for k, vv := range v {
			_, isMap := vv.(map[string]interface{})
			_, isArray := vv.([]interface{})
			if isMap || isArray {
				out[i] = fmt.Sprintf("%s/\n", k)
			} else {
				out[i] = fmt.Sprintf("%s\n", k)
			}
			i++
		}

		sort.Strings(out)
		for _, vv := range out {
			fmt.Fprint(w, vv)
		}
	case []interface{}:
		for k, vv := range v {
			vvMap, isMap := vv.(map[string]interface{})
			_, isArray := vv.([]interface{})

			if isMap {
				// If the child is a map and has a "name" property, show "key=name"
				name, ok := vvMap[MAGIC_ARRAY_KEY]
				if ok {
					fmt.Fprintf(w, "%d=%s\n", k, name)
					continue
				}
			}

			if isMap || isArray {
				// If the child is a map or array, show "key/"
				fmt.Fprintf(w, "%d/\n", k)
			} else {
				// Otherwise, show "key"
				fmt.Fprintf(w, "%d\n", k)
			}
		}
	default:
		http.Error(w, "Value is of a type I don't know how to handle", http.StatusInternalServerError)
	}
}

func respondJSON(w http.ResponseWriter, req *http.Request, val interface{}) {
	bytes, err := json.Marshal(val)
	if err == nil {
		w.Write(bytes)
	} else {
		respondError(w, req, "Error serializing to JSON: "+err.Error(), http.StatusInternalServerError)
	}
}

func respondYAML(w http.ResponseWriter, req *http.Request, val interface{}) {
	bytes, err := yaml.Marshal(val)
	if err == nil {
		w.Write(bytes)
	} else {
		respondError(w, req, "Error serializing to YAML: "+err.Error(), http.StatusInternalServerError)
	}
}

func requestIp(req *http.Request) string {
	if *enableXff {
		clientIp := req.Header.Get("X-Forwarded-For")
		if len(clientIp) > 0 {
			return clientIp
		}
	}

	clientIp, _, _ := net.SplitHostPort(req.RemoteAddr)
	return clientIp
}
