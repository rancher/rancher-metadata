package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/mitchellh/mapstructure"
	revents "github.com/rancher/event-subscriber/events"
	"github.com/rancher/go-rancher/v2"
	"github.com/rancher/rancher-metadata/pkg/kicker"
)

type ReloadFunc func(versions Versions)

var Delta *MetadataDelta
var SavedVersion string

type Subscriber struct {
	url        string
	accessKey  string
	secretKey  string
	reload     ReloadFunc
	answerFile string
	client     *http.Client
	kicker     *kicker.Kicker
}

func init() {
	var data []map[string]interface{}
	Delta = &MetadataDelta{
		Version: "0",
		Data:    data,
	}
}

func NewSubscriber(url, accessKey, secretKey, answerFile string, reload ReloadFunc) *Subscriber {
	s := &Subscriber{
		url:        url,
		accessKey:  accessKey,
		secretKey:  secretKey,
		reload:     reload,
		answerFile: answerFile,
		client:     &http.Client{},
	}
	s.kicker = kicker.New(func() {
		if err := s.downloadAndReload(); err != nil {
			logrus.Errorf("Failed to download and reload metadata: %v", err)
		}
	})
	return s
}

func (s *Subscriber) Subscribe() error {
	handlers := map[string]revents.EventHandler{
		"ping":          s.noOp,
		"config.update": s.configUpdate,
	}

	router, err := revents.NewEventRouter("", 0, s.url, s.accessKey, s.secretKey, nil, handlers, "", 3, revents.DefaultPingConfig)
	if err != nil {
		return err
	}

	go func() {
		sp := revents.SkippingWorkerPool(3, nil)
		for {
			s.kicker.Kick()
			if err := router.RunWithWorkerPool(sp); err != nil {
				logrus.Errorf("Exiting subscriber: %v", err)
			}
			time.Sleep(time.Second)
		}
	}()

	go func() {
		for t := range time.Tick(30 * time.Second) {
			s.saveToFile(t)
		}
	}()

	return nil
}

func (s *Subscriber) noOp(event *revents.Event, c *client.RancherClient) error {
	return nil
}

func (s *Subscriber) configUpdate(event *revents.Event, c *client.RancherClient) error {
	update := ConfigUpdateData{}
	if err := mapstructure.Decode(event.Data, &update); err != nil {
		return err
	}

	found := false
	i := 0
	for _, item := range update.Items {
		if found = item.Name == "metadata-answers"; found {
			logrus.Info("Update requested for version: %d", item.RequestedVersion)
			i = s.kicker.Kick()
			break
		}
	}

	if i > 0 {
		s.kicker.Wait(i)
	}

	_, err := c.Publish.Create(&client.Publish{
		Name:        event.ReplyTo,
		PreviousIds: []string{event.ID},
	})
	return err
}

func (s *Subscriber) saveDeltaToFile() error {
	tempFile := s.answerFile + ".temp"
	out, err := os.Create(tempFile)
	if err != nil {
		return err
	}
	defer func() {
		out.Close()
		os.Remove(tempFile)
	}()

	err = json.NewEncoder(out).Encode(Delta)
	if err != nil {
		return err
	}

	os.Rename(tempFile, s.answerFile)
	return nil
}

func (s *Subscriber) downloadAndReload() error {
	url := s.url + "/configcontent/metadata-answers?client=v2"
	// 1. Download meta
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(s.accessKey, s.secretKey)
	start := time.Now()
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	logrus.Infof("Downloaded in %s", time.Since(start))

	if resp.StatusCode != 200 {
		content, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("non-200 response %d: %s", resp.StatusCode, content)
	}

	// 2. Decode the delta
	logrus.Infof("Generating and reloading answers")
	err = GenerateDelta(resp.Body)
	if err != nil {
		logrus.Errorf("Failed to decode delta")
		return err
	}

	logrus.Infof("Generating answers")
	// 3. Geneate answers
	versions, err := GenerateAnswers(Delta)
	if err != nil {
		logrus.Errorf("Failed to generate answers")
		return err
	}

	// 4. Reload
	s.reload(versions)
	logrus.Infof("Generated and reloaded answers")

	// 5. Generate a reply
	def, ok := versions["latest"]["default"].(map[string]interface{})
	if ok {
		version, _ := def["version"].(string)
		logrus.Infof("Applied %s", url+"?version="+version)
		req, err := http.NewRequest("PUT", url+"?client=v2&version="+version, nil)
		if err != nil {
			return err
		}

		req.SetBasicAuth(s.accessKey, s.secretKey)
		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		if resp.Body != nil {
			resp.Body.Close()
		}
	} else {
		return fmt.Errorf("Failed to locate default version")
	}

	logrus.Infof("Download and reload in: %v", time.Since(start))

	return nil
}

type ConfigUpdateData struct {
	ConfigUrl string
	Items     []ConfigUpdateItem
}

type ConfigUpdateItem struct {
	Name             string
	RequestedVersion int
}

func GenerateDelta(body io.ReadCloser) error {
	dec := json.NewDecoder(body)
	dec.UseNumber()
	var data []map[string]interface{}
	var version string
	for {
		var o map[string]interface{}
		err := dec.Decode(&o)
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		} else {
			data = append(data, o)
			kind := o["metadata_kind"]
			if kind == "defaultData" {
				version = o["version"].(string)
			}
		}
	}
	reloadDelta(version, data)
	return nil
}

func reloadDelta(version string, data []map[string]interface{}) {
	Delta.Lock()
	defer Delta.Unlock()
	Delta.Version = version
	Delta.Data = data
}

func (s *Subscriber) saveToFile(t time.Time) {
	Delta.Lock()
	defer Delta.Unlock()
	currentVersion := Delta.Version
	if SavedVersion != Delta.Version && len(Delta.Data) > 0 {
		err := s.saveDeltaToFile()
		if err != nil {
			logrus.Errorf("Failed to save delta to file: [%v]", err)
		} else {
			SavedVersion = currentVersion
			logrus.Debugf("Saved delta to file at [%v]", t)
		}
	}
}
