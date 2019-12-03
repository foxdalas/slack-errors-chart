package main

import (
	"fmt"
	"github.com/ashwanthkumar/slack-go-webhook"
	"github.com/foxdalas/slack-errors-chart/pkg/elastic"
	"log"
	"os"
	"strings"
	"time"
)

func main () {
	client, err := elastic.New(strings.Split(os.Getenv("ELASTICSEARCH"), ","))
    if err != nil {
    	log.Fatal(err)
	}

	data, err := elastic.GetErrors(client.Ctx, client.Client)
	if err != nil {
		log.Fatal(err)
	}

	layoutISO := "2006-01-02"

	head := fmt.Sprintf("Вчера *%s* было запросов\n*%d* всего\n", time.Now().AddDate(0, 0, -1).Format(layoutISO), data.Total)
	head += fmt.Sprintf("*%d* ошибок *(%.2f%%)*\n\n", data.Errors,data.ErrorsPercent)
	head += fmt.Sprint("Топ 10 команд за вчера\n")

	for id, ns := range data.Namespaces {
		if id >= 9 {
			continue
		}
		weekAgo := ((float64(ns.Count) - float64(ns.WeekAgoCount)) / float64(ns.WeekAgoCount)) * 100
		head += fmt.Sprintf("*%s* *%d* ошибок *(%.2f%%)* %d неделю назад\n", ns.Namespace, ns.Count,weekAgo,ns.WeekAgoCount)
	}

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	head += fmt.Sprint("\n\n")
	head += fmt.Sprintf("Top 10 Ingress\n")
	for id, ing := range data.Results {
		if id >= 9 {
			continue
		}
		kibanaUrl := fmt.Sprint(os.Getenv("KIBANA")+"/app/kibana#/discover?_g=(refreshInterval:(pause:!t,value:0),time:(from:'"+yesterday+"T00:00:00.000Z',to:'"+yesterday+"T23:59:59.000Z'))&_a=(columns:!(vhost,request,status),interval:auto,query:(language:kuery,query:'ingress_name:%20\""+ing.Ingress+"\"%20AND%20status%20%3E%20499%20AND%20NOT%20region:%20\"dev\"'),sort:!(!('@timestamp',desc)))")
		head += fmt.Sprintf("*%s* ошибок <%s|*%d*>\n", ing.Ingress,kibanaUrl, ing.Errors)
	}

	var message []string

	for _, out := range data.Results {
		message = append(message, fmt.Sprintf("Ingress: %s errors: %d\n", out.Ingress, out.Errors))
	}

	webhookUrl := os.Getenv("SLACK")
	payload := slack.Payload {
		Text: head,
		Username: "Максим",
		Channel: "#dops-public",
	}
	er := slack.Send(webhookUrl, "", payload)
	if len(er) > 0 {
		fmt.Printf("error: %s\n", err)
	}
}
