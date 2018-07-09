package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/codegangsta/cli"
	"github.com/golang/gddo/httputil"
	"github.com/gorilla/mux"
	"github.com/rancher/log"
	logserver "github.com/rancher/log/server"
	"github.com/rancher/rancher-metadata/config"
	"github.com/rancher/rancher-metadata/server"
	"gopkg.in/yaml.v2"
)

const (
	ContentText = 1
	ContentJSON = 2
	ContentYAML = 3
)

var (
	VERSION string
	// A key to check for magic traversing of arrays by a string field in them
	// For example, given: { things: [ {name: 'asdf', stuff: 42}, {name: 'zxcv', stuff: 43} ] }
	// Both ../things/0/stuff and ../things/asdf/stuff will return 42 because 'asdf' matched the 'anme' field of one of the 'things'.
)

// ServerConfig specifies the configuration for the metadata server
type ServerConfig struct {
	sync.Mutex

	metadataController *server.MetadataController

	listen       string
	listenReload string
	enableXff    bool

	router       *mux.Router
	reloadRouter *mux.Router
	reloadChan   chan chan error
}

func main() {
	logserver.StartServerWithDefaults()
	app := getCliApp()
	app.Action = appMain
	app.Run(os.Args)
}

func getCliApp() *cli.App {
	app := cli.NewApp()
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Debug",
		},
		cli.BoolFlag{
			Name:  "xff",
			Usage: "X-Forwarded-For header support",
		},
		cli.StringFlag{
			Name:  "listen",
			Value: ":80",
			Usage: "Address to listen to (TCP)",
		},
		cli.StringFlag{
			Name:  "listenReload",
			Value: "127.0.0.1:8112",
			Usage: "Address to listen to for reload requests (TCP)",
		},
		cli.StringFlag{
			Name:  "answers",
			Value: "./answers.json",
			Usage: "File containing the answers to respond with",
		},
		cli.StringFlag{
			Name:  "log",
			Value: "",
			Usage: "Log file",
		},
		cli.StringFlag{
			Name:  "pid-file",
			Value: "",
			Usage: "PID to write to",
		},
		cli.BoolFlag{
			Name:  "subscribe",
			Usage: "Subscribe to Rancher events",
		},
		cli.Int64Flag{
			Name:  "reload-interval-limit",
			Usage: "Limits reload to 1 per interval (milliseconds)",
			Value: 1000,
		},
	}

	return app
}

func appMain(ctx *cli.Context) error {
	if ctx.GlobalBool("debug") {
		log.SetLevelString("debug")
	}

	logFile := ctx.GlobalString("log")
	if logFile != "" {
		if output, err := os.OpenFile(logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666); err != nil {
			log.Fatalf("Failed to log to file %s: %v", logFile, err)
		} else {
			log.SetOutput(output)
		}
	}

	pidFile := ctx.GlobalString("pid-file")
	if pidFile != "" {
		log.Infof("Writing pid %d to %s", os.Getpid(), pidFile)
		if err := ioutil.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
			log.Fatalf("Failed to write pid file %s: %v", pidFile, err)
		}
	}

	sc := NewServerConfig(
		ctx.GlobalString("listen"),
		ctx.GlobalString("listenReload"),
		ctx.GlobalBool("xff"),
		ctx.GlobalBool("subscribe"),
		ctx.GlobalString("answers"),
		ctx.Int64("reload-interval-limit"),
	)

	if err := sc.StartServer(); err != nil {
		return err
	}

	// Run the server
	sc.RunServer()

	return nil
}

func (sc *ServerConfig) StartServer() error {
	if err := sc.metadataController.Start(); err != nil {
		return err
	}
	go func() {
		log.Info(http.ListenAndServe(":6060", nil))
	}()
	return nil
}

func NewServerConfig(listen, listenReload string, enableXff bool, subscribe bool, answers string, reloadInterval int64) *ServerConfig {
	router := mux.NewRouter()
	reloadRouter := mux.NewRouter()
	reloadChan := make(chan chan error)
	return &ServerConfig{
		listen:             listen,
		listenReload:       listenReload,
		enableXff:          enableXff,
		router:             router,
		reloadRouter:       reloadRouter,
		reloadChan:         reloadChan,
		metadataController: server.NewMetadataController(subscribe, answers, reloadInterval),
	}
}

func (sc *ServerConfig) watchSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for _ = range c {
			log.Info("Received HUP signal")
			sc.reloadChan <- nil
		}
	}()

	go func() {
		for resp := range sc.reloadChan {
			err := sc.metadataController.LoadVersionsFromFile()
			if resp != nil {
				resp <- err
			}
		}
	}()

}

func (sc *ServerConfig) watchHttp() {
	sc.reloadRouter.HandleFunc("/favicon.ico", http.NotFound)
	sc.reloadRouter.HandleFunc("/v1/reload", sc.httpReload).Methods("POST")

	log.Info("Listening for Reload on ", sc.listenReload)
	go http.ListenAndServe(sc.listenReload, sc.reloadRouter)
}

func (sc *ServerConfig) RunServer() {
	sc.watchSignals()
	sc.watchHttp()

	sc.router.HandleFunc("/favicon.ico", http.NotFound)
	sc.router.HandleFunc("/", sc.root).
		Methods("GET", "HEAD").
		Name("Root")

	sc.router.HandleFunc("/{version}", sc.metadata).
		Methods("GET", "HEAD").
		Name("Version")

	sc.router.HandleFunc("/{version}/{key:.*}", sc.metadata).
		Queries("wait", "true", "value", "{oldValue}").
		Methods("GET", "HEAD").
		Name("Wait")

	sc.router.HandleFunc("/{version}/{key:.*}", sc.metadata).
		Methods("GET", "HEAD").
		Name("Metadata")

	log.Info("Listening on ", sc.listen)
	log.Fatal(http.ListenAndServe(sc.listen, sc.router))
}

func (sc *ServerConfig) httpReload(w http.ResponseWriter, req *http.Request) {
	log.Debugf("Received HTTP reload request")
	respChan := make(chan error)
	sc.reloadChan <- respChan
	err := <-respChan

	if err == nil {
		io.WriteString(w, "OK")
	} else {
		w.WriteHeader(500)
		io.WriteString(w, err.Error())
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

func (sc *ServerConfig) root(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	log.Debugf("OK: %s client=%v version=%v", "/", sc.requestIp(req), "root")

	answers := sc.metadataController.GetVersions()

	m := make(map[string]interface{})
	for _, k := range answers.Versions() {
		url, err := sc.router.Get("Version").URL("version", k)
		if err == nil {
			m[k] = (*url).String()
		} else {
			log.Warn("Error: ", err.Error())
		}
	}

	// If latest isn't in the list, pretend it is
	_, ok := m["latest"]
	if !ok {
		url, err := sc.router.Get("Version").URL("version", "latest")
		if err == nil {
			m["latest"] = (*url).String()
		} else {
			log.Warn("Error: ", err.Error())
		}
	}

	respondSuccess(w, req, m)
}

func (sc *ServerConfig) metadata(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	vars := mux.Vars(req)
	clientIp := sc.requestIp(req)

	version := vars["version"]
	wait := mux.CurrentRoute(req).GetName() == "Wait"
	oldValue := vars["oldValue"]
	maxWait, _ := strconv.Atoi(req.URL.Query().Get("maxWait"))

	answers := sc.metadataController.GetVersions()
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

	path := strings.TrimRight(req.URL.EscapedPath()[1:], "/")
	pathSegments := strings.Split(path, "/")[1:]
	displayKey := ""
	var err error
	for i := 0; err == nil && i < len(pathSegments); i++ {
		displayKey += "/" + pathSegments[i]
		pathSegments[i], err = url.QueryUnescape(pathSegments[i])
	}

	if err != nil {
		respondError(w, req, err.Error(), http.StatusBadRequest)
		return
	}

	log.Debugf("Searching for: %s version=%v client=%v wait=%v oldValue=%v maxWait=%v", displayKey, version, clientIp, wait, oldValue, maxWait)
	val, ok := sc.metadataController.LookupAnswer(wait, oldValue, version, clientIp, pathSegments, time.Duration(maxWait)*time.Second)

	if ok {
		log.Debugf("OK: %s version=%v client=%v", displayKey, version, clientIp)
		respondSuccess(w, req, val)
	} else {
		log.Infof("Error: %s version=%v client=%v", displayKey, version, clientIp)
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
	case string, json.Number:
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
				out[i] = fmt.Sprintf("%s/\n", url.QueryEscape(k))
			} else {
				out[i] = fmt.Sprintf("%s\n", url.QueryEscape(k))
			}
			i++
		}

		sort.Strings(out)
		for _, vv := range out {
			fmt.Fprint(w, vv)
		}
	case []interface{}:
	outer:
		for k, vv := range v {
			vvMap, isMap := vv.(map[string]interface{})
			_, isArray := vv.([]interface{})

			if isMap {
				// If the child is a map and has a "name" property, show index=name ("0=foo")
				for _, magicKey := range config.MAGIC_ARRAY_KEYS {
					name, ok := vvMap[magicKey]
					if ok {
						fmt.Fprintf(w, "%d=%s\n", k, url.QueryEscape(name.(string)))
						continue outer
					}
				}
			}

			if isMap || isArray {
				// If the child is a map or array, show index ("0/")
				fmt.Fprintf(w, "%d/\n", k)
			} else {
				// Otherwise, show index ("0" )
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

func (sc *ServerConfig) requestIp(req *http.Request) string {
	if sc.enableXff {
		clientIp := req.Header.Get("X-Forwarded-For")
		if len(clientIp) > 0 {
			return clientIp
		}
	}

	clientIp, _, _ := net.SplitHostPort(req.RemoteAddr)
	return clientIp
}
