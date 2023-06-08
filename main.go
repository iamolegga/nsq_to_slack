package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nsqio/go-nsq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

var (
	topics    multiFlag
	channels  multiFlag
	addr      = flag.String("nsqd-tcp-address", "localhost:4150", "nsqd TCP address")
	lookupd   = flag.String("lookupd-http-address", "", "nsqlookupd HTTP address")
	goMetrics = flag.Bool("gom", false, "Expose Go runtime metrics")
	logLevel  = zap.LevelFlag("log", zap.InfoLevel, "log level (debug, info, warn, error, dpanic, panic, fatal)")
	slackURL  = flag.String("slack-webhook-url", "", "Slack webhook URL")
)

type multiFlag []string

func (mf *multiFlag) String() string {
	return fmt.Sprint(*mf)
}

func (mf *multiFlag) Set(value string) error {
	*mf = append(*mf, value)
	return nil
}

func main() {
	var err error
	flag.Var(&topics, "topic", "NSQ topic (may be given multiple times)")
	flag.Var(&channels, "channel", "NSQ channel (may be given multiple times)")
	flag.Parse()

	if len(topics) != len(channels) {
		panic("the number of topics and channels must be the same")
	}

	// logger setup
	zapCfg := zap.NewProductionConfig()
	zapCfg.Level = zap.NewAtomicLevelAt(*logLevel)
	zapCfg.Encoding = "json"
	zapCfg.DisableCaller = true
	zapCfg.DisableStacktrace = true
	zapCfg.OutputPaths = []string{"stdout"}
	zapCfg.ErrorOutputPaths = []string{"stderr"}
	zapCfg.EncoderConfig = zap.NewProductionEncoderConfig()
	logger, err := zapCfg.Build(zap.Fields(zap.String("app", "nsq_to_slack")))
	if err != nil {
		panic(err)
	}

	logger.Debug(
		"starting nsq_to_slack",
		// flags
		zap.Strings("topics", topics),
		zap.Strings("channels", channels),
		zap.String("nsqd-tcp-address", *addr),
		zap.String("lookupd-http-address", *lookupd),
		zap.Bool("gom", *goMetrics),
		zap.String("log", zapCfg.Level.String()),
		zap.String("slack-webhook-url", *slackURL),
	)

	// metrics setup
	customRegistry := prometheus.NewRegistry()
	nsqMsgs := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nsq_messages_total",
			Help: "Number of NSQ messages.",
			ConstLabels: prometheus.Labels{
				"app":  "nsq_to_slack",
				"host": strings.Split(*addr, ":")[0],
			},
		},
		[]string{"status", "topic", "channel"},
	)
	customRegistry.MustRegister(nsqMsgs)
	if *goMetrics {
		customRegistry.MustRegister(collectors.NewGoCollector())
	}
	http.Handle("/metrics", promhttp.HandlerFor(customRegistry, promhttp.HandlerOpts{}))

	// slack setup
	if *slackURL == "" {
		logger.Fatal("slack webhook URL is required")
	}
	sender := &slackSender{
		webhookUrl: *slackURL,
		logger:     logger,
	}

	// nsq setup
	config := nsq.NewConfig()
	var nsqLogLevel = nsq.LogLevelInfo
	switch *logLevel {
	case zap.DebugLevel:
		nsqLogLevel = nsq.LogLevelDebug
	case zap.InfoLevel:
		nsqLogLevel = nsq.LogLevelInfo
	case zap.WarnLevel:
		nsqLogLevel = nsq.LogLevelWarning
	case zap.ErrorLevel:
		nsqLogLevel = nsq.LogLevelError
	case zap.DPanicLevel:
		nsqLogLevel = nsq.LogLevelError
	case zap.PanicLevel:
		nsqLogLevel = nsq.LogLevelError
	case zap.FatalLevel:
		nsqLogLevel = nsq.LogLevelError
	}
	nsqZapLogger := &nsqZapLogger{logger: logger}
	var consumers []*nsq.Consumer

	for i, topic := range topics {
		channel := channels[i]
		consumer, err := nsq.NewConsumer(topic, channel, config)
		if err != nil {
			logger.Fatal("failed to create NSQ consumer", zap.Error(err))
		}
		consumer.SetLogger(nsqZapLogger, nsqLogLevel)

		consumer.AddHandler(&nsqHandler{
			logger:  logger,
			metrics: nsqMsgs,
			sender:  sender,
			topic:   topic,
			channel: channel,
		})

		if *lookupd != "" {
			err = consumer.ConnectToNSQLookupd(*lookupd)
		} else {
			err = consumer.ConnectToNSQD(*addr)
		}
		if err != nil {
			logger.Fatal("failed to connect to NSQ", zap.Error(err))
		}
		logger.Info("connected to NSQ", zap.String("topic", topic), zap.String("channel", channel))
		consumers = append(consumers, consumer)
	}

	go func() {
		httpAddr := ":9090" // replace with your desired port
		logger.Info("starting HTTP server", zap.String("addr", httpAddr))
		if err := http.ListenAndServe(httpAddr, nil); err != nil {
			logger.Fatal("failed to start HTTP server", zap.Error(err))
		}
	}()

	// graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	for _, consumer := range consumers {
		consumer.Stop()
	}
}

type nsqHandler struct {
	logger  *zap.Logger
	metrics *prometheus.CounterVec
	sender  *slackSender
	topic   string
	channel string
}

func (h *nsqHandler) HandleMessage(message *nsq.Message) error {
	id := string(message.ID[:])
	attempts := fmt.Sprintf("%d", message.Attempts)

	l := h.logger.With(
		zap.String("nsq_topic", h.topic),
		zap.String("nsq_channel", h.channel),
		zap.String("id", id),
		zap.String("attempts", attempts),
	)
	l.Debug("message received")

	if err := h.sender.Send(message.Body, slackSenderMeta{
		topic:    h.topic,
		channel:  h.channel,
		id:       id,
		Attempts: message.Attempts,
	}); err != nil {
		h.metrics.WithLabelValues("error", h.topic, h.channel).Inc()
		l.Error("message failed to send", zap.Error(err))
		return nil
	}

	h.metrics.WithLabelValues("ok", h.topic, h.channel).Inc()
	l.Info("message sent")
	return nil
}

type slackSender struct {
	webhookUrl string
	logger     *zap.Logger
}

type slackSenderMeta struct {
	topic    string
	channel  string
	id       string
	Attempts uint16
}

func (s *slackSender) Send(message []byte, meta slackSenderMeta) error {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, message, "", "  "); err != nil {
		s.logger.Warn("failed to pretty print JSON", zap.Error(err))
	}
	var msg string
	if prettyJSON.Len() > 0 {
		msg = prettyJSON.String()
	} else {
		msg = string(message)
	}

	payload := map[string]interface{}{
		"blocks": []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]interface{}{
					"type": "mrkdwn",
					"text": fmt.Sprintf(":envelope: *%s*/*%s*", meta.topic, meta.channel),
				},
			},
			{
				"type": "section",
				"text": map[string]interface{}{
					"type": "mrkdwn",
					"text": fmt.Sprintf("```%s```", msg),
				},
			},
			{
				"type": "context",
				"elements": []map[string]interface{}{
					{
						"type": "plain_text",
						"text": fmt.Sprintf("#%s | attempt %d", meta.id, meta.Attempts),
					},
				},
			},
		},
	}

	jsonPayload, err := json.Marshal(payload)
	resp, err := http.Post(s.webhookUrl, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return fmt.Errorf("failed to send message to Slack: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send message to Slack: %s", resp.Status)
	}
	return nil
}

type nsqZapLogger struct {
	logger *zap.Logger
}

func (n *nsqZapLogger) Output(_ int, s string) error {
	// Split the log line into parts.
	parts := strings.Fields(s)
	if len(parts) < 2 {
		return fmt.Errorf("failed to parse NSQ log line: %s", s)
	}

	level := parts[0]
	message := strings.Join(parts[1:], " ")

	// Remove any connection or producer ID from the message.
	message = strings.TrimLeft(message, "0123456789 ")

	// Map the NSQ log level to a Zap log level.
	var logFunc func(string, ...zap.Field)
	switch level {
	case "DBG":
		logFunc = n.logger.Debug
	case "INF":
		logFunc = n.logger.Info
	case "WRN":
		logFunc = n.logger.Warn
	case "ERR":
		logFunc = n.logger.Error
	default:
		logFunc = n.logger.Info
	}

	logFunc(message)
	return nil
}
