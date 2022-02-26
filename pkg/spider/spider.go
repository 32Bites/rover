package spider

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
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
}

// Spider defines an instance of a web crawler.
type Spider struct {
	OnCrawl CrawledSiteHandler
	Logger  *log.Logger
	state   struct {
		*sync.RWMutex
		current SpiderState
	}
	sites          chan Site
	sitesWaitGroup *sync.WaitGroup
	results        chan CrawledSite
}

// CrawledSiteHandler is a closure type that defines a function that is called upon a spider crawling a site.
type CrawledSiteHandler func(site CrawledSite, spider *Spider)

// New creates a new spider
// workerCount defines how many workers for both crawling and handling crawled sites.
func New(workerCount uint, logger *log.Logger, onCrawl CrawledSiteHandler) (*Spider, error) {
	if workerCount == 0 {
		return nil, errors.New("cannot create a spider with zero workers")
	}

	if logger == nil {
		logger = log.Default()
	}

	if onCrawl == nil {
		onCrawl = func(site CrawledSite, spider *Spider) {}
	}

	spider := &Spider{
		OnCrawl:        onCrawl,
		Logger:         logger,
		sites:          make(chan Site, workerCount),
		sitesWaitGroup: &sync.WaitGroup{},
		results:        make(chan CrawledSite, workerCount),
		state: struct {
			*sync.RWMutex
			current SpiderState
		}{
			RWMutex: &sync.RWMutex{},
			current: StateCrawling,
		},
	}

	for i := 0; i < int(workerCount); i++ {
		go spider.siteWorker(i)
		spider.sitesWaitGroup.Add(1)
		go spider.resultsWorker(i)
	}

	return spider, nil
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

// Send allows you to externally send urls to the spider for handling
func (s *Spider) Send(urls ...string) (err error) {
	for _, url := range urls {
		time.Sleep(time.Millisecond)
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
	workerId := fmt.Sprintf("Crawl-%d", id)
	for site := range s.sites {
		s.Logger.Println(workerId, site.URL)
		s.results <- CrawledSite{Site: &site}
	}

	s.sitesWaitGroup.Done()
}

func (s *Spider) resultsWorker(id int) {
	workerId := fmt.Sprintf("Result-%d", id)
	for result := range s.results {
		s.Logger.Println(workerId, result)
		s.OnCrawl(result, s)
	}
}
