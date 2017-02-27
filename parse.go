package main

import (
	"bytes"
	"encoding/json"
	"os"

	log "github.com/Sirupsen/logrus"
)

func ParseAnswers(path string) (out Versions, err error) {
	var v Versions
	var md MetadataDelta
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("Failed to find: ", path)
			return v, nil
		}
		return nil, err
	}
	defer f.Close()

	err = json.NewDecoder(f).Decode(&md)
	if err != nil {
		return nil, err
	}

	delta, err := GenerateDelta(bytes.NewBuffer(md.Data))
	if err != nil {
		return v, err
	}

	v, err = GenerateAnswers(delta)
	if err != nil {
		return v, err
	}

	return v, err
}
