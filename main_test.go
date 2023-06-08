package main_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/nsqio/go-nsq"
	"github.com/stretchr/testify/suite"
)

type Suite struct {
	suite.Suite
	topics           []string
	channels         []string
	args             []string
	producer         *nsq.Producer
	receivedMessages chan []byte
	slackServer      *httptest.Server
	cmd              *exec.Cmd
	nsqMessage       map[string]int64
}

func (s *Suite) SetupSuite() {
	s.T().Logf("setting up suite")
	s.slackServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		readBody, err := io.ReadAll(r.Body)
		s.Nil(err, "failed to read request body")
		s.receivedMessages <- readBody
	}))
	s.T().Logf("slack server listening on %s", s.slackServer.URL)
	config := nsq.NewConfig()
	var err error
	s.producer, err = nsq.NewProducer("localhost:4150", config)
	s.Nil(err, "failed to create nsq producer")
	s.T().Logf("suite setup complete")
}

func (s *Suite) SetupTest() {
	s.topics = []string{}
	s.channels = []string{}
	s.args = []string{
		"-slack-webhook-url", s.slackServer.URL,
		"-log", "debug",
	}
	s.receivedMessages = make(chan []byte)

	s.nsqMessage = map[string]int64{"ts": time.Now().Unix()}

	s.T().Logf("test setup complete")
}

func (s *Suite) TearDownTest() {
	err := s.cmd.Process.Kill()
	s.Nil(err, "failed to kill nsq_to_slack process")
	close(s.receivedMessages)
	s.T().Logf("test teardown complete")
}

func (s *Suite) TearDownSuite() {
	s.slackServer.Close()
	s.producer.Stop()
	s.T().Logf("suite teardown complete")
}

func (s *Suite) TestSingleChannel() {
	s.setupTopicsChannels("foo-topic", "bar-channel")
	s.runNsqToSlack()

	payload, err := json.Marshal(s.nsqMessage)
	s.Nil(err, "failed to marshal json payload")
	err = s.producer.Publish(s.topics[0], payload)
	s.Nil(err, "failed to publish message")
	s.T().Logf("published message: %s", string(payload))

	s.verifyMessage(<-s.receivedMessages)
}

func (s *Suite) TestMultipleChannels() {
	s.setupTopicsChannels("foo-topic", "bar-channel", "baz-topic", "qux-channel")
	s.runNsqToSlack()

	payload, err := json.Marshal(s.nsqMessage)
	s.Nil(err, "failed to marshal json payload")

	err = s.producer.Publish(s.topics[0], payload)
	s.Nil(err, "failed to publish message")
	s.T().Logf("published message: %s", string(payload))
	s.verifyMessage(<-s.receivedMessages)

	err = s.producer.Publish(s.topics[1], payload)
	s.Nil(err, "failed to publish message")
	s.T().Logf("published message: %s", string(payload))
	s.verifyMessage(<-s.receivedMessages)
}

func (s *Suite) setupTopicsChannels(names ...string) {
	prefix := time.Now().Format("20060102150405")
	for i := 0; i < len(names); i++ {
		topic := names[i] + "_" + prefix
		i++
		channel := names[i] + "_" + prefix
		s.topics = append(s.topics, topic)
		s.channels = append(s.channels, channel)
		s.args = append(s.args, "-topic", topic)
		s.args = append(s.args, "-channel", channel)
	}
	s.T().Logf("topics: %v", s.topics)
}

func (s *Suite) runNsqToSlack() {
	s.cmd = exec.Command("./nsq_to_slack", s.args...)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr
	err := s.cmd.Start()
	s.Nil(err, "failed to run nsq_to_slack")
	time.Sleep(time.Second)
	s.T().Logf("nsq_to_slack running with args: %v", s.args)
}

func (s *Suite) verifyMessage(slackIncomingMsg []byte) {
	msgStr := string(slackIncomingMsg)
	s.T().Logf("received message: %s", msgStr)

	var slackMsg map[string][]interface{}
	err := json.Unmarshal(slackIncomingMsg, &slackMsg)
	s.Nil(err, "failed to unmarshal slack message")

	// dive to the nested text field
	blocks := slackMsg["blocks"]
	snippetBlock := blocks[1]
	snippetIfc := (snippetBlock.(map[string]interface{})["text"]).(map[string]interface{})["text"]
	snippet := snippetIfc.(string)
	snippet = strings.Replace(snippet, "```", "", -1)

	var payloadParsed map[string]int64
	err = json.Unmarshal([]byte(snippet), &payloadParsed)
	s.Nil(err, "failed to unmarshal payload")

	s.Equal(s.nsqMessage, payloadParsed, "slack payload does not match nsq")
}

func TestSuite(t *testing.T) {
	suite.Run(t, new(Suite))
}
