package spider

import (
	"errors"
	"fmt"
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
}

// Spider defines an instance of a web crawler.
type Spider struct {
	OnCrawl     CrawledSiteHandler
	Logger      *log.Logger
	WorkerCount uint
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

// New creates a new spider
// workerCount defines how many workers for both crawling and handling crawled sites.
func New(workerCount uint, args ...interface{}) (*Spider, error) {
	if workerCount == 0 {
		return nil, errors.New("cannot create a spider with zero workers")
	}

	spider := &Spider{
		OnCrawl:        func(site CrawledSite, spider *Spider) {},
		Logger:         log.Default(),
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
	}

	return spider, nil
}

func (s *Spider) Start() {
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
	}
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

func (s *Spider) SendSitesMap(sites map[string]Site) (err error) {
	for _, site := range sites {
		time.Sleep(time.Millisecond)
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

func (s *Spider) SendSites(sites ...Site) (err error) {
	for _, site := range sites {
		time.Sleep(time.Millisecond)
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

func (s *Spider) log(worker, message string) {
	s.Logger.Printf("[%s] - %s\n", worker, message)
}

func (s *Spider) siteWorker(id int) {
	workerId := fmt.Sprintf("Crawl-%d", id)
	for site := range s.sites {
		client := http.Client{}
		resp, err := client.Get(site.URL)
		if err != nil {
			continue
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			s.log(workerId, err.Error())
			continue
		}

		thisURL, err := url.Parse(site.URL)
		if err != nil {
			continue
		}
		thisURLString := thisURL.String()

		foundSites := map[string]Site{}

		doc.Find("[href]").Each(
			func(i int, sel *goquery.Selection) {
				if value, exists := sel.Attr("href"); exists {
					currentURL, err := thisURL.Parse(value)
					if err != nil {
						return
					}
					currentURLString := currentURL.String()

					foundSites[currentURLString] = Site{URL: currentURLString, FoundAt: &thisURLString}
				}
			},
		)
		go s.SendSitesMap(foundSites)
		s.results <- CrawledSite{Site: &site, CrawledAt: time.Now()}
	}

	s.sitesWaitGroup.Done()
}

func (s *Spider) resultsWorker(id int) {
	workerId := fmt.Sprintf("Result-%d", id)
	for result := range s.results {
		s.Logger.Println(workerId, *result.Site)
		s.OnCrawl(result, s)
	}
}
