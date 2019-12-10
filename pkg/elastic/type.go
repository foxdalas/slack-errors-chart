package elastic

import (
	"github.com/olivere/elastic/v7"
	"golang.org/x/net/context"
)

type elasticSearch struct {
	Ctx    context.Context
	Client *elastic.Client
	index  string
}

type EsRetrier struct {
	backoff elastic.Backoff
}
