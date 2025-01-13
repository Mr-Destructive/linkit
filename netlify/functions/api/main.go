package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"os"
	"strconv"

	_ "github.com/tursodatabase/libsql-client-go/libsql"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/mr-destructive/linkit/embedsql"
	"github.com/mr-destructive/linkit/models"
)

var (
	queries      *models.Queries
	linkTemplate *template.Template
	listTemplate *template.Template
)

func main() {
	lambda.Start(handler)
}

func handler(req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {

	ctx := context.Background()
	dbName := os.Getenv("DB_NAME")
	dbToken := os.Getenv("DB_TOKEN")

	var err error
	dbString := fmt.Sprintf("libsql://%s?authToken=%s", dbName, dbToken)
	db, err := sql.Open("libsql", dbString)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}
	defer db.Close()

	queries = models.New(db)
	if _, err := db.ExecContext(ctx, embedsql.DDL); err != nil {
		log.Printf("error creating tables: %v", err)
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}

	linkTemplate = template.Must(template.ParseFiles("link.html"))
	listTemplate = template.Must(template.ParseFiles("list.html"))

	switch req.HTTPMethod {
	case "GET":
		var links []models.Link
		links, err = queries.ListLinks(ctx)
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 500}, err
		}
		return respond(req, links)
	case "POST":
		var link models.CreateLinkParams
		err = json.Unmarshal([]byte(req.Body), &link)
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 400, Body: "Invalid request body"}, nil
		}
		createdLinkId, err := queries.CreateLink(ctx, link)
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		createdLink, err := queries.GetLink(ctx, createdLinkId)

		return respond(req, createdLink)
	case "DELETE":
		linkIdStr, ok := req.PathParameters["id"]
		if !ok {
			return events.APIGatewayProxyResponse{StatusCode: 400, Body: "Missing link ID"}, nil
		}
		linkId, err := strconv.Atoi(linkIdStr)
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 400, Body: "Invalid link ID"}, nil
		}
		err = queries.DeleteLink(ctx, int64(linkId))
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: err.Error()}, nil
		}
		return events.APIGatewayProxyResponse{StatusCode: 200}, nil
	default:
		return events.APIGatewayProxyResponse{StatusCode: 405, Body: "Method Not Allowed"}, nil
	}
}

func respond(req events.APIGatewayProxyRequest, data any) (events.APIGatewayProxyResponse, error) {
	if req.Headers["X-Requested-With"] == "HTMX" {
		htmlFragment, err := generateHTMLFragment(data)
		if err != nil {
			return events.APIGatewayProxyResponse{StatusCode: 500}, err
		}
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "text/html"},
			Body:       htmlFragment,
		}, nil
	}
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500}, err
	}
	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(dataBytes),
	}, nil
}

func generateHTMLFragment(data any) (string, error) {
	var tpl bytes.Buffer

	switch v := data.(type) {
	case []models.Link:
		err := listTemplate.Execute(&tpl, v)
		if err != nil {
			return "", err
		}
	case models.Link:
		err := linkTemplate.Execute(&tpl, v)
		if err != nil {
			return "", err
		}
	default:
		return "", fmt.Errorf("unsupported data type for HTML fragment generation: %T", data)
	}

	return tpl.String(), nil
}
