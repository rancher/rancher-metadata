package main

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"bytes"
	log "github.com/Sirupsen/logrus"
)

func ParseAnswers(path string) (out Versions, err error) {
	var delta *MetadataDelta
	var v Versions
	data, err := ioutil.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Warn("Failed to find: ", path)
			return v, nil
		}
		return nil, err
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	err = dec.Decode(&delta)
	if err != nil {
		return v, err
	}

	v, err = GenerateAnswers(delta)
	if err != nil {
		return v, err
	}

	return v, err
}
