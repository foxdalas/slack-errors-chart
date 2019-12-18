package elastic

import (
	"context"
	"errors"
	"fmt"
	elastic "github.com/olivere/elastic/v7"
	"net/http"
	"syscall"
	"time"
)

type Request struct {
	Request string
	Errors  int64
}

type Namespace struct {
	Namespace    string
	Count        int64
	WeekAgoCount int64
}

type Stats struct {
	Total         int64
	Errors        int64
	ErrorsPercent float64
	Results       []*Result
	Namespaces    []*Namespace
}

type Result struct {
	Ingress  string
	Requests []*Request
	Errors   int64
}

func New(elasticHost []string) (*elasticSearch, error) {
	client, err := elastic.NewClient(
		elastic.SetURL(elasticHost...),
		elastic.SetSniff(false),
		elastic.SetRetrier(NewEsRetrier()),
		elastic.SetHealthcheck(true),
		elastic.SetHealthcheckTimeout(time.Second*300),
	)
	if err != nil {
		return nil, err
	}

	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)

	return &elasticSearch{
		Client: client,
		Ctx:    ctx,
	}, nil
}

func (e *elasticSearch) searchResults(query *elastic.BoolQuery, aggregationString *elastic.TermsAggregation, aggregationName string, date string) (*elastic.SearchResult, error) {
	searchResult, err := e.Client.Search().
		Index("nginx-"+date). // search in index
		Query(query).         // specify the query
		Size(0).
		Aggregation(aggregationName, aggregationString).
		Pretty(true).
		Do(e.Ctx)
	return searchResult, err
}


func (e *elasticSearch) vhostAggregation(searchResult *elastic.SearchResult, stats Stats) Stats {
	vhost, found := searchResult.Aggregations.Terms("vhost")
	if found {
		for _, b := range vhost.Buckets {
			result := &Result{
				Ingress: b.Key.(string),
				Errors:  b.DocCount,
			}
			request, f := b.Aggregations.Terms("by_request")
			if f {
				for _, rb := range request.Buckets {
					stats.Total += rb.DocCount
					req := &Request{
						Request: rb.Key.(string),
						Errors:  rb.DocCount,
					}
					result.Requests = append(result.Requests, req)
				}
			}
			stats.Results = append(stats.Results, result)
		}
	}
	return stats
}

func (e *elasticSearch) namespaceCount(searchResult *elastic.SearchResult) map[string]int64 {
	namespaces := make(map[string]int64)
	namespace, found := searchResult.Aggregations.Terms("namespace")
	if found {
		for _, n := range namespace.Buckets {
			namespaces[n.Key.(string)] = n.DocCount
		}
	}
	return namespaces
}

func (e *elasticSearch) GetErrors(ctx context.Context, elasticClient *elastic.Client) (Stats, error) {
	var stats Stats

	layoutISO := "2006.01.02"
	yesterday := time.Now().AddDate(0, 0, -1).Format(layoutISO)
	weekAgo := time.Now().AddDate(0, 0, -7).Format(layoutISO)

	errQuery := elastic.NewRangeQuery("status").From(500).To(599)
	dev := elastic.NewTermQuery("region", "dev")

	aggregationName := "vhost"
	aggr := elastic.NewTermsAggregation().Field("ingress_name.keyword").Size(1000)

	aggrigationNamespace := "namespace"
	aggerNamespace := elastic.NewTermsAggregation().Field("namespace.keyword")

	generalQ := elastic.NewBoolQuery()
	generalQ = generalQ.Must(errQuery).MustNot(dev)

	searchResult, err := e.searchResults(generalQ, aggr, aggregationName, yesterday)
	if err != nil {
		return stats, err
	}
	weekAgoSearchResult, err := e.searchResults(generalQ, aggerNamespace, aggrigationNamespace, weekAgo)
	if err != nil {
		return stats, err
	}

	namespacesWeekAgo := make(map[string]int64)
	namespace, found := weekAgoSearchResult.Aggregations.Terms("namespace")
	fmt.Print(found)
	if found {
		for _, n := range namespace.Buckets {
			namespacesWeekAgo[n.Key.(string)] = n.DocCount
		}
	}


	stats = e.vhostAggregation(searchResult, stats)

	count, err := elasticClient.Count("nginx-" + yesterday).Do(ctx)
	if err != nil {
		return stats, err
	}

	errors, err := elasticClient.Count("nginx-" + yesterday).Query(generalQ).Do(ctx)
	if err != nil {
		return stats, err
	}

	stats.Total = count
	stats.Errors = errors
	stats.ErrorsPercent = (float64(errors) / float64(count)) * 100

	searchResult, err = e.searchResults(generalQ, aggerNamespace, aggrigationNamespace, yesterday)
	if err != nil {
		return stats, err
	}

	namespace, found = searchResult.Aggregations.Terms("namespace")
	if found {
		for _, n := range namespace.Buckets {
			ns := &Namespace{
				n.Key.(string),
				n.DocCount,
				namespacesWeekAgo[n.Key.(string)],
			}
			stats.Namespaces = append(stats.Namespaces, ns)
		}
	}
	return stats, err
}

func NewEsRetrier() *EsRetrier {
	return &EsRetrier{
		backoff: elastic.NewExponentialBackoff(10*time.Millisecond, 8*time.Second),
	}
}

func (r *EsRetrier) Retry(ctx context.Context, retry int, req *http.Request, resp *http.Response, err error) (time.Duration, bool, error) {
	// Fail hard on a specific error
	if err == syscall.ECONNREFUSED {
		return 0, false, errors.New("Elasticsearch or network down")
	}

	// Stop after 5 retries
	if retry >= 5 {
		return 0, false, nil
	}

	// Let the backoff strategy decide how long to wait and whether to stop
	wait, stop := r.backoff.Next(retry)
	return wait, stop, nil
}
