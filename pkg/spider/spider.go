package spider

import (
	"bytes"
	"errors"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// SpiderState represents the current state of the spider.
type SpiderState uint

const (
	StateCrawling = SpiderState(0)
	StateStopped  = SpiderState(1)
)

// Site represents a found site.
type Site struct {
	URL     string
	FoundAt *string
}

// CrawledSite respresents a crawled site.
type CrawledSite struct {
	*Site
	CrawledAt time.Time
	Response  *http.Response
}

// Spider defines an instance of a web crawler.
type Spider struct {
	OnCrawl     CrawledSiteHandler
	Logger      *log.Logger
	WorkerCount uint
	SendDelay   *time.Duration
	CrawlDelay  *time.Duration
	ShouldCrawl ShouldCrawlURLHandler
	state       struct {
		*sync.RWMutex
		current SpiderState
	}
	sites          chan Site
	sitesWaitGroup *sync.WaitGroup
	results        chan CrawledSite
}

// CrawledSiteHandler is a closure type that defines a function that is called upon a spider crawling a site.
type CrawledSiteHandler func(site CrawledSite, spider *Spider)

// ShouldCrawlURLHandler is a closure type that defines a function that is called upon to check whether or not the spider should crawl a url.
type ShouldCrawlURLHandler func(foundAt, url *url.URL, spider *Spider) bool

// New creates a new spider
// workerCount defines how many workers for both crawling and handling crawled sites.
func New(workerCount uint, args ...interface{}) (*Spider, error) {
	if workerCount == 0 {
		return nil, errors.New("cannot create a spider with zero workers")
	}

	spider := &Spider{
		OnCrawl:        func(site CrawledSite, spider *Spider) {},
		ShouldCrawl:    func(foundAt, url *url.URL, spider *Spider) bool { return true },
		Logger:         log.Default(),
		SendDelay:      nil,
		CrawlDelay:     nil,
		WorkerCount:    workerCount,
		sitesWaitGroup: &sync.WaitGroup{},
		state: struct {
			*sync.RWMutex
			current SpiderState
		}{
			RWMutex: &sync.RWMutex{},
			current: StateStopped,
		},
	}

	for _, arg := range args {
		if logger, ok := arg.(*log.Logger); ok {
			spider.Logger = logger
		}

		if onCrawl, ok := arg.(func(site CrawledSite, spider *Spider)); ok {
			spider.OnCrawl = onCrawl
		}

		if shouldCrawl, ok := arg.(func(foundAt, url *url.URL, spider *Spider) bool); ok {
			spider.ShouldCrawl = shouldCrawl
		}

		if duration, ok := arg.(time.Duration); ok {
			if spider.SendDelay == nil {
				spider.SendDelay = &duration
			} else {
				spider.CrawlDelay = &duration
			}
		}
	}

	if spider.SendDelay == nil {
		duration := time.Millisecond
		spider.SendDelay = &duration
	}

	if spider.CrawlDelay == nil {
		duration := time.Second / 2
		spider.CrawlDelay = &duration
	}

	return spider, nil
}

// Start will start the spider.
func (s *Spider) Start() error {
	s.state.Lock()
	defer s.state.Unlock()
	if s.state.current == StateStopped {
		s.results = make(chan CrawledSite, s.WorkerCount)
		s.sites = make(chan Site, s.WorkerCount)

		for i := 0; i < int(s.WorkerCount); i++ {
			go s.siteWorker(i)
			s.sitesWaitGroup.Add(1)
			go s.resultsWorker(i)
		}
		s.state.current = StateCrawling
		return nil
	}

	return errors.New("cannot start spider that is already started")
}

// Stop safely stops the spider.
func (s *Spider) Stop() {
	s.state.Lock()
	defer s.state.Unlock()
	close(s.sites)
	s.sitesWaitGroup.Wait()
	close(s.results)
	s.state.current = StateStopped
}

// SendSitesMap allows you to externally send urls to the spider for handling
func (s *Spider) SendSitesMap(sites map[string]Site) (err error) {
	for _, site := range sites {
		time.Sleep(*s.SendDelay)
		s.state.Lock()
		if s.state.current != StateCrawling {
			s.state.Unlock()
			err = errors.New("cannot send urls to stopped spider")
			return
		}
		s.sites <- site
		s.state.Unlock()
	}
	return
}

// SendSites allows you to externally send urls to the spider for handling
func (s *Spider) SendSites(sites ...Site) (err error) {
	for _, site := range sites {
		time.Sleep(*s.SendDelay)
		s.state.Lock()
		if s.state.current != StateCrawling {
			s.state.Unlock()
			err = errors.New("cannot send urls to stopped spider")
			return
		}
		s.sites <- site
		s.state.Unlock()
	}
	return
}

// Send allows you to externally send urls to the spider for handling
func (s *Spider) Send(urls ...string) (err error) {
	for _, url := range urls {
		time.Sleep(*s.SendDelay)
		s.state.Lock()
		if s.state.current != StateCrawling {
			s.state.Unlock()
			err = errors.New("cannot send urls to stopped spider")
			return
		}
		s.sites <- Site{URL: url}
		s.state.Unlock()
	}
	return
}

func (s *Spider) siteWorker(id int) {
	// workerId := fmt.Sprintf("Crawl-%d", id)
	for site := range s.sites {
		client := http.Client{}
		resp, err := client.Get(site.URL)
		if err != nil {
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		resp.Body = io.NopCloser(bytes.NewBuffer(body))

		doc, err := goquery.NewDocumentFromReader(
			ioutil.NopCloser(
				bytes.NewBuffer(body),
			),
		)

		if err != nil {
			continue
		}

		url, err := url.Parse(site.URL)
		if err != nil {
			continue
		}

		go s.SendSitesMap(
			s.findURLs(url, doc),
		)

		s.results <- CrawledSite{Site: &site, CrawledAt: time.Now(), Response: resp}
		time.Sleep(*s.CrawlDelay)
	}

	s.sitesWaitGroup.Done()
}

func (s *Spider) resultsWorker(id int) {
	// workerId := fmt.Sprintf("Result-%d", id)
	for result := range s.results {
		s.OnCrawl(result, s)
	}
}

func (s *Spider) findURLs(url *url.URL, doc *goquery.Document) map[string]Site {
	foundSites := map[string]Site{}
	urlString := url.String()

	doc.Find("[href]").Each(
		func(_ int, sel *goquery.Selection) {
			if value, exists := sel.Attr("href"); exists {
				currentURL, err := url.Parse(value)
				if err != nil {
					return
				}

				if !s.ShouldCrawl(url, currentURL, s) {
					return
				}

				currentURLString := currentURL.String()
				foundSites[currentURLString] = Site{URL: currentURLString, FoundAt: &urlString}
			}
		},
	)

	return foundSites
}
