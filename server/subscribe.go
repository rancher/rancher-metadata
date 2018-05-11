package server

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juju/ratelimit"
	"github.com/leodotcloud/log"
	"github.com/mitchellh/mapstructure"
	revents "github.com/rancher/event-subscriber/events"
	"github.com/rancher/go-rancher/v2"
	"github.com/rancher/rancher-metadata/config"
	"github.com/rancher/rancher-metadata/pkg/kicker"
)

type ReloadFunc func(versions config.Versions, creds []config.Credential, version string)

type Subscriber struct {
	url                  string
	accessKey            string
	secretKey            string
	reload               ReloadFunc
	client               *http.Client
	kicker               *kicker.Kicker
	router               *revents.EventRouter
	generator            *config.Generator
	requestedVersion     string
	requestedVersionLock sync.Mutex
	reloadInterval       int64
	limiter              *ratelimit.Bucket
	stopCh               chan struct{}
}

func formatUrl(url string) string {
	if strings.HasSuffix(url, "/v1") || strings.HasSuffix(url, "/v2-beta") {
		return url
	}
	if strings.HasSuffix(url, "/") {
		return fmt.Sprintf("%s%s", url, "v2-beta")
	}
	return fmt.Sprintf("%s%s", url, "/v2-beta")
}

func NewSubscriber(url string, accessKey string, secretKey string, generator *config.Generator, reloadInterval int64, reload ReloadFunc) *Subscriber {
	s := &Subscriber{
		url:            formatUrl(url),
		accessKey:      accessKey,
		secretKey:      secretKey,
		reload:         reload,
		client:         &http.Client{},
		generator:      generator,
		reloadInterval: reloadInterval,
		limiter:        ratelimit.NewBucketWithQuantum(time.Duration(reloadInterval)*time.Millisecond, 1.0, 1),
		stopCh:         make(chan struct{}),
	}
	s.kicker = kicker.New(func() {
		if err := s.downloadAndReload(); err != nil {
			log.Errorf("Failed to download and reload metadata: %v url=%v access_key=%v", err, s.url, s.accessKey)
		}
	})
	return s
}

func (s *Subscriber) SetRequestedVersion(version string) {
	s.requestedVersionLock.Lock()
	s.requestedVersion = version
	s.requestedVersionLock.Unlock()
}

func (s *Subscriber) GetRequestedVersion() string {
	s.requestedVersionLock.Lock()
	defer s.requestedVersionLock.Unlock()
	if s.requestedVersion == "0" || len(s.requestedVersion) == 0 {
		return ""
	}
	return s.requestedVersion
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

	s.router = router

	go func() {
		sp := revents.SkippingWorkerPool(3, nil)
		for {
			s.kicker.Kick()
			if err := s.router.RunWithWorkerPool(sp); err != nil {
				log.Errorf("Exiting subscriber: %v url=%v access_key=%v", err, s.url, s.accessKey)
			}
			select {
			case <-s.stopCh:
				return
			default:
				time.Sleep(time.Second)
			}
		}
	}()

	go func() {
		for t := range time.Tick(30 * time.Second) {
			s.generator.SaveToFile(t)
			select {
			case <-s.stopCh:
				return
			default:
				time.Sleep(time.Second)
			}
		}
	}()

	return nil
}

func (s *Subscriber) Unsubscribe() {
	close(s.stopCh)
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
			log.Infof("Update requested for version: %d url=%v access_key=%v", item.RequestedVersion, s.url, s.accessKey)
			s.SetRequestedVersion(strconv.Itoa(item.RequestedVersion))
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

func (s *Subscriber) downloadAndReload() error {
	s.limiter.WaitMaxDuration(1, time.Duration(s.reloadInterval)*time.Millisecond)
	log.Infof("Downloading metadata")
	url := s.url + "/configcontent/metadata-answers?client=v2&requestedVersion=" + s.GetRequestedVersion()
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
	log.Infof("Downloaded in %s", time.Since(start))

	if resp.StatusCode != 200 {
		content, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("non-200 response %d: %s", resp.StatusCode, content)
	}

	// 2. Decode the delta
	log.Infof("Generating and reloading answers")
	delta, version, err := s.generator.GenerateDelta(resp.Body)
	if err != nil {
		log.Errorf("Failed to decode delta")
		return err
	}

	log.Infof("Generating answers")
	// 3. Geneate answers
	versions, creds, err := s.generator.GenerateAnswers(delta)
	if err != nil {
		log.Errorf("Failed to generate answers")
		return err
	}

	// 4. Reload
	s.reload(versions, creds, version)
	log.Infof("Generated and reloaded answers")

	// 5. Generate a reply
	log.Infof("Applied %s", url+"?version="+version)
	req, err = http.NewRequest("PUT", url+"?client=v2&version="+version, nil)
	if err != nil {
		return err
	}

	req.SetBasicAuth(s.accessKey, s.secretKey)
	resp, err = s.client.Do(req)
	if err != nil {
		return err
	}
	if resp.Body != nil {
		resp.Body.Close()
	}

	log.Infof("Download and reload in: %v", time.Since(start))

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
