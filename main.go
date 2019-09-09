package main

import (
	"cloud.google.com/go/pubsub"
	"context"
	"encoding/json"
	"fmt"
	"github.com/domodwyer/mailyak"
	"github.com/urfave/cli"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	html_template "html/template"
	"log"
	"net"
	"net/smtp"
	"os"
	"os/signal"
	"sort"
	"strings"
	text_template "text/template"
	"time"
)

type budgetNotification struct {
	BudgetDisplayName      string  `json:"budgetDisplayName"`
	AlertThresholdExceeded float64 `json:"alertThresholdExceeded"`
	CostAmount             float64 `json:"costAmount"`
	CostIntervalStart      string  `json:"costIntervalStart"`
	BudgetAmount           float64 `json:"budgetAmount"`
	BudgetAmountType       string  `json:"budgetAmountType"`
	CurrencyCode           string  `json:"currencyCode"`
}

type callbackHandler struct {
	logger        *zap.Logger
	global        []string
	budget        map[string][]string
	emailHost     string
	emailPort     string
	emailUser     string
	emailPassword string
	emailFrom     string
}

var Version string
var BuildTime string

var projectFlag = cli.StringFlag{
	Name:   "project",
	Usage:  "The GCP project",
	EnvVar: "GCP_PROJECT",
}

var topicFlag = cli.StringFlag{
	Name:   "topic",
	Usage:  "The topic to subscribe to.",
	EnvVar: "GCP_TOPIC",
}

var subscriptionFlag = cli.StringFlag{
	Name:   "subscription",
	Usage:  "The id of the subscription.",
	EnvVar: "GCP_SUBSCRIPTION",
}

var recipientFlag = cli.StringSliceFlag{
	Name:   "recipient",
	Usage:  "A recipient of a notification",
	EnvVar: "RECIPIENTS",
}

var emailHostFlag = cli.StringFlag{
	Name:   "email-host",
	Usage:  "The email server to connect to.",
	EnvVar: "EMAIL_HOST",
}

var emailPortFlag = cli.StringFlag{
	Name:   "email-port",
	Usage:  "The email host port.",
	EnvVar: "EMAIL_PORT",
}

var emailUserFlag = cli.StringFlag{
	Name:   "email-user",
	Usage:  "The user to authenticate as",
	EnvVar: "EMAIL_USER",
}

var emailPasswordFlag = cli.StringFlag{
	Name:   "email-password",
	Usage:  "The password of the email user",
	EnvVar: "EMAIL_PASSWORD",
}

var emailFromFlag = cli.StringFlag{
	Name:   "email-from",
	Usage:  "The sender of the email",
	EnvVar: "EMAIL_FROM",
}

var textTemplate = `BudgetDisplayName: {{.BudgetDisplayName}}
AlertThresholdExceeded: {{.AlertThresholdExceeded}}
CostAmount: {{.CostAmount}}
CostIntervalStart: {{.CostIntervalStart}}
BudgetAmount: {{.BudgetAmount}}
BudgetAmountType: {{.BudgetAmountType}}
CurrencyCode: {{.CurrencyCode}}`

var htmlTemplate = `<table>
<tr><td>BudgetDisplayName</td><td>{{.BudgetDisplayName}}</td></tr>
<tr><td>AlertThresholdExceeded</td><td>{{.AlertThresholdExceeded}}</td></tr>
<tr><td>CostAmount</td><td>{{.CostAmount}}</td></tr>
<tr><td>CostIntervalStart</td><td>{{.CostIntervalStart}}</td></tr>
<tr><td>BudgetAmount</td><td>{{.BudgetAmount}}</td></tr>
<tr><td>BudgetAmountType</td><td>{{.BudgetAmountType}}</td></tr>
<tr><td>CurrencyCode</td><td>{{.CurrencyCode}}</td></tr>
</table>`

func main() {
	compiledAt, err := time.Parse(time.RFC822Z, BuildTime)
	if err != nil {
		compiledAt = time.Now()
	}

	app := cli.NewApp()
	app.Name = "gcp-budget-notifier"
	app.HelpName = "gcp-budget-notifier"
	app.Usage = "A budget notifier for GCP."
	app.Version = Version
	app.Compiled = compiledAt
	app.Copyright = "(c) 2019 Nick Gerakines <nick@gerakines.net>"
	app.Flags = []cli.Flag{
		emailFromFlag,
		emailPasswordFlag,
		emailUserFlag,
		emailPortFlag,
		emailHostFlag,
		recipientFlag,
		subscriptionFlag,
		topicFlag,
		projectFlag,
	}
	app.Action = serverCommand

	sort.Sort(cli.FlagsByName(app.Flags))
	sort.Sort(cli.CommandsByName(app.Commands))

	err = app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func serverCommand(cliCtx *cli.Context) error {
	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("can't initialize zap logger: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g, gctx := errgroup.WithContext(ctx)

	logger.Info("starting")

	client, err := pubsub.NewClient(ctx, cliCtx.String("project"))
	if err != nil {
		return err
	}

	topic := client.Topic(cliCtx.String("topic"))
	ok, err := topic.Exists(ctx);
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("topic does not exist: %s", cliCtx.String(topicFlag.Name))
	}

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	g.Go(func() error {
		logger.Info("starting signal listener")
		<-quit
		cancel()
		return nil
	})

	global, budget := parseBudgetWatchers(cliCtx.StringSlice(recipientFlag.Name))

	handler := callbackHandler{
		logger:        logger,
		global:        global,
		budget:        budget,
		emailHost:     cliCtx.String(emailHostFlag.Name),
		emailPort:     cliCtx.String(emailPortFlag.Name),
		emailUser:     cliCtx.String(emailUserFlag.Name),
		emailPassword: cliCtx.String(emailPasswordFlag.Name),
		emailFrom:     cliCtx.String(emailFromFlag.Name),
	}

	g.Go(func() error {
		sub, err := client.CreateSubscription(ctx, cliCtx.String("subscription"), pubsub.SubscriptionConfig{Topic: topic})
		if err != nil {
			return err
		}
		if err := sub.Receive(gctx, handler.callback); err != context.Canceled {
			return err
		}
		return nil
	})

	if err := g.Wait(); err != nil {
		logger.Error("error caught", zap.Error(err))
		return err
	}

	logger.Info("stopping")
	return nil
}

func emailFieldsFunc(c rune) bool {
	return c == ';'
}

func parseBudgetWatchers(recipients []string) ([]string, map[string][]string) {
	global := []string{}
	budget := map[string][]string{}
	for _, recipient := range recipients {
		parts := strings.FieldsFunc(recipient, emailFieldsFunc)
		if len(parts) > 0 {
			if len(parts) == 1 {
				global = append(global, recipient)
				continue
			}
			budget[parts[0]] = parts[1:]
		}

	}
	return global, budget
}

func (h callbackHandler) callback(ctx context.Context, message *pubsub.Message) {
	message.Ack();
	budgetId, ok := message.Attributes["budgetId"]
	if !ok {
		h.logger.Error("budgetId not found in attributes")
		return
	}

	var notification budgetNotification
	if err := json.Unmarshal(message.Data, &notification); err != nil {
		h.logger.Error("error parsing message data", zap.Error(err))
		return
	}

	to := h.global
	if others, ok := h.budget[budgetId]; ok {
		to = append(to, others...)
	}
	if len(to) == 0 {
		h.logger.Warn("no recipients for budget notification")
		return
	}

	address := net.JoinHostPort(h.emailHost, h.emailPort)

	mail := mailyak.New(address, smtp.PlainAuth("", h.emailUser, h.emailPassword, h.emailHost))

	mail.To(to...)
	mail.From(h.emailFrom)

	mail.Subject(fmt.Sprintf("Budget alert: %s", notification.BudgetDisplayName))

	textTemplate, err := text_template.New("text").Parse(textTemplate)
	if err != nil {
		h.logger.Error("could not create text body", zap.Error(err))
	}

	htmlTemplate, err := html_template.New("html").Parse(htmlTemplate)
	if err != nil {
		h.logger.Error("could not create html body", zap.Error(err))
	}

	if err := textTemplate.ExecuteTemplate(mail.Plain(), "text", notification); err != nil {
		h.logger.Error("could not render text body to message", zap.Error(err))
		return
	}

	if err := htmlTemplate.ExecuteTemplate(mail.Plain(), "html", notification); err != nil {
		h.logger.Error("could not render html body to message", zap.Error(err))
		return
	}

	if err := mail.Send(); err != nil {
		h.logger.Error("could not send message", zap.Error(err))
	}
}
