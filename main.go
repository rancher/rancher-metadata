package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

var (
	debug       = flag.Bool("debug", false, "Debug")
	listen      = flag.String("listen", ":53", "Address to listen to (TCP and UDP)")
	answersFile = flag.String("answers", "./answers.json", "File containing the answers to respond with")
	logFile     = flag.String("log", "", "Log file")
	pidFile     = flag.String("pid-file", "", "PID to write to")

	versions = []string{"latest", "2015-07-31"}
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

	router := mux.NewRouter()
	router.HandleFunc("/favicon.ico", http.NotFound)
	router.HandleFunc("/", root).Methods("GET")

	for _, version := range versions {
		router.HandleFunc("/{version:"+version+"}", metadata).Methods("GET")
		router.HandleFunc("/{version:"+version+"}/{key:.*}", metadata).Methods("GET")
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
		answers = temp
		log.Info("Loaded answers for ", len(answers), " IPs")
	} else {
		log.Errorf("Failed to load answers: %v", err)
	}

	return err
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

func root(w http.ResponseWriter, req *http.Request) {
	for _, version := range versions {
		fmt.Fprintf(w, "%s\n", version)
	}
}

func metadata(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	clientIp, _, _ := net.SplitHostPort(req.RemoteAddr)
	wholePath := req.URL.Path
	key := vars["key"]

	log.WithFields(log.Fields{"client": clientIp, "version": vars["version"]}).Debug("Request for ", key)
	fmt.Fprintf(w, "Hello world [%s]: %s %s\n", clientIp, wholePath, key)
}
