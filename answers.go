package main

import (
	log "github.com/Sirupsen/logrus"
)

// The top-level key in the JSON for the default (not client-specific answers)
const DEFAULT_KEY = "default"

func (answers *Answers) Matching(path []string, clientIp string) (values interface{}, ok bool) {
	log.WithFields(log.Fields{"client": clientIp}).Debug("Matching: ", path)
	return nil, false
}
