package spider

import (
	"errors"
	"fmt"
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
	state   struct {
		*sync.RWMutex
		current SpiderState
	}
	sites            chan Site
	results          chan CrawledSite
	resultsWaitGroup *sync.WaitGroup
}

// CrawledSiteHandler is a closure type that defines a function that is called upon a spider crawling a site.
type CrawledSiteHandler func(site CrawledSite, spider *Spider)

// New creates a new spider
// workerCount defines how many workers for both crawling and handling crawled sites.
func New(workerCount uint, onCrawl CrawledSiteHandler) (*Spider, error) {
	if workerCount == 0 {
		return nil, errors.New("cannot create a spider with zero workers")
	}

	if onCrawl == nil {
		onCrawl = func(site CrawledSite, spider *Spider) {}
	}

	spider := &Spider{
		OnCrawl:          onCrawl,
		sites:            make(chan Site, workerCount),
		results:          make(chan CrawledSite, workerCount),
		resultsWaitGroup: &sync.WaitGroup{},
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
		go spider.resultsWorker(i)
		spider.resultsWaitGroup.Add(1)
	}

	return spider, nil
}

// Stop safely stops the spider.
func (s *Spider) Stop() {
	s.state.Lock()
	defer s.state.Unlock()
	close(s.sites)
	s.resultsWaitGroup.Wait()
	close(s.results)
	s.state.current = StateStopped
}

// Send allows you to externally send urls to the spider for handling
func (s *Spider) Send(urls ...string) (err error) {
	for _, url := range urls {
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
		fmt.Println(workerId, site.URL)
	}
}

func (s *Spider) resultsWorker(id int) {
	workerId := fmt.Sprintf("Result-%d", id)
	for result := range s.results {
		fmt.Println(workerId, result)
		s.OnCrawl(result, s)
	}

	s.resultsWaitGroup.Done()
}
