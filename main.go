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
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/golang/gddo/httputil"
	"github.com/gorilla/mux"
)

const (
	ContentText = 1
	ContentJSON = 2

	// The top-level key in the JSON for the default (not client-specific answers)
	DEFAULT_KEY = "default"

	// A key to check for magic traversing of arrays by a string field in them
	// For example, given: { things: [ {name: 'asdf', stuff: 42}, {name: 'zxcv', stuff: 43} ] }
	// Both ../things/0/stuff and ../things/asdf/stuff will return 42 because 'asdf' matched the 'anme' field of one of the 'things'.
	MAGIC_ARRAY_KEY = "name"
)

var (
	debug       = flag.Bool("debug", false, "Debug")
	listen      = flag.String("listen", ":80", "Address to listen to (TCP)")
	answersFile = flag.String("answers", "./answers.json", "File containing the answers to respond with")
	logFile     = flag.String("log", "", "Log file")
	pidFile     = flag.String("pid-file", "", "PID to write to")

	versions = []string{"latest", "2015-07-25"}
	router   = mux.NewRouter()
	answers  Answers
)

func main() {
	log.Info("Starting rancher-metadata")
	parseFlags()
	err := loadAnswers()
	if err != nil {
		log.Fatal("Cannot startup without a valid Answers file")
	}
	watchSignals()

	router.HandleFunc("/favicon.ico", http.NotFound)
	router.HandleFunc("/", root).
		Methods("GET", "HEAD").
		Name("Root")

	for _, version := range versions {
		router.HandleFunc("/{version:"+version+"}", metadata).
			Methods("GET", "HEAD").
			Name("Version:" + version)

		router.HandleFunc("/{version:"+version+"}/", metadata).
			Methods("GET", "HEAD").
			Name("Version:" + version)

		router.HandleFunc("/{version:"+version+"}/{key:.*}", metadata).
			Methods("GET", "HEAD").
			Name("Metadata")

		router.HandleFunc("/{version:"+version+"}/{key:.*}/", metadata).
			Methods("GET", "HEAD").
			Name("Metadata")
	}

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
	temp, err := ParseAnswers(*answersFile)
	if err == nil {
		defaults, ok := temp[DEFAULT_KEY]
		if ok {
			defaultsMap, ok := defaults.(map[string]interface{})
			if ok {
				// Copy the defaults into the per-client info, so there's no
				// complicated merging and lookup logic when retrieving.
				mergeDefaults(&temp, defaultsMap)
			}
		}

		answers = temp
		log.Infof("Loaded answers for %d IPs", len(answers))
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
			log.Info("Received HUP signal, reloading answers")
			loadAnswers()
		}
	}()
}

func contentType(req *http.Request) int {
	str := httputil.NegotiateContentType(req, []string{"text/plain", "application/json"}, "text/plain")
	if str == "application/json" {
		return ContentJSON
	} else {
		return ContentText
	}
}

func root(w http.ResponseWriter, req *http.Request) {
	clientIp, _, _ := net.SplitHostPort(req.RemoteAddr)

	log.WithFields(log.Fields{"client": clientIp, "version": "root"}).Infof("OK: %s", "/")
	m := make(map[string]interface{})
	for _, version := range versions {
		url, err := router.Get("Version:"+version).URL("version", version)
		if err == nil {
			m[version] = (*url).String()
		} else {
			log.Warn("Error: ", err.Error())
		}
	}
	respondSuccess(w, req, m)
}

func metadata(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	clientIp, _, _ := net.SplitHostPort(req.RemoteAddr)

	key := vars["key"]
	displayKey := "/" + key

	val, ok := answers.Matching(key, clientIp)

	if ok {
		log.WithFields(log.Fields{"client": clientIp, "version": vars["version"]}).Infof("OK: %s", displayKey)
		respondSuccess(w, req, val)
	} else {
		log.WithFields(log.Fields{"client": clientIp, "version": vars["version"]}).Infof("Error: %s", displayKey)
		respondError(w, req, "Not found", http.StatusNotFound)
	}
}

func respondError(w http.ResponseWriter, req *http.Request, msg string, statusCode int) {
	switch contentType(req) {
	case ContentText:
		http.Error(w, msg, statusCode)
	case ContentJSON:
		obj := make(map[string]interface{})
		obj["message"] = msg
		obj["type"] = "error"
		obj["code"] = statusCode
		bytes, err := json.Marshal(obj)
		if err == nil {
			http.Error(w, string(bytes), statusCode)
		} else {
			http.Error(w, "{\"message\": \"JSON marshal error\"}", http.StatusInternalServerError)
		}
	}
}

func respondSuccess(w http.ResponseWriter, req *http.Request, val interface{}) {
	switch contentType(req) {
	case ContentText:
		respondText(w, req, val)
	case ContentJSON:
		respondJSON(w, req, val)
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
	case float32, float64, complex64, complex128:
		fmt.Fprintf(w, "%g", v)
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
		http.Error(w, "Error serializing to JSON:"+err.Error(), http.StatusInternalServerError)
	}
}
